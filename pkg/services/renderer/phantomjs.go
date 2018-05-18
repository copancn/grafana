package renderer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/grafana/grafana/pkg/log"
	"github.com/grafana/grafana/pkg/middleware"
	"github.com/grafana/grafana/pkg/setting"
)

func (rs *RenderService) renderViaPhantomJS(ctx context.Context, opts Opts) (*RenderResult, error) {
	rs.log.Info("Rendering", "path", opts.Path)

	var executable = "phantomjs"
	if runtime.GOOS == "windows" {
		executable = executable + ".exe"
	}

	url := rs.getURL(opts.Path)
	binPath, _ := filepath.Abs(filepath.Join(setting.PhantomDir, executable))
	scriptPath, _ := filepath.Abs(filepath.Join(setting.PhantomDir, "render.js"))
	pngPath := rs.getFilePathForNewImage()

	renderKey := middleware.AddRenderAuthKey(opts.OrgId, opts.UserId, opts.OrgRole)
	defer middleware.RemoveRenderAuthKey(renderKey)

	phantomDebugArg := "--debug=false"
	if log.GetLogLevelFor("renderer") >= log.LvlDebug {
		phantomDebugArg = "--debug=true"
	}

	cmdArgs := []string{
		"--ignore-ssl-errors=true",
		"--web-security=false",
		phantomDebugArg,
		scriptPath,
		fmt.Sprintf("url=%v", url),
		fmt.Sprintf("width=%v", opts.Width),
		fmt.Sprintf("height=%v", opts.Height),
		fmt.Sprintf("png=%v", pngPath),
		fmt.Sprintf("domain=%v", rs.getLocalDomain()),
		fmt.Sprintf("timeout=%v", opts.Timeout.Seconds()),
		fmt.Sprintf("renderKey=%v", renderKey),
	}

	if opts.Encoding != "" {
		cmdArgs = append([]string{fmt.Sprintf("--output-encoding=%s", opts.Encoding)}, cmdArgs...)
	}

	commandCtx, _ := context.WithTimeout(ctx, opts.Timeout+time.Second*2)
	cmd := exec.CommandContext(commandCtx, binPath, cmdArgs...)
	cmd.Stderr = cmd.Stdout

	if opts.Timezone != "" {
		baseEnviron := os.Environ()
		cmd.Env = appendEnviron(baseEnviron, "TZ", isoTimeOffsetToPosixTz(opts.Timezone))
	}

	out, err := cmd.Output()

	// check for timeout first
	if ctx.Err() == context.DeadlineExceeded {
		rs.log.Info("Rendering timed out")
		return nil, ErrTimeout
	}

	if err != nil {
		rs.log.Error("Phantomjs exited with non zero exit code", "error", err)
		return nil, err
	}

	rs.log.Debug("Phantomjs output", "out", string(out))

	rs.log.Debug("Image rendered", "path", pngPath)
	return &RenderResult{FilePath: pngPath}, nil
}

func isoTimeOffsetToPosixTz(isoOffset string) string {
	// invert offset
	if strings.HasPrefix(isoOffset, "UTC+") {
		return strings.Replace(isoOffset, "UTC+", "UTC-", 1)
	}
	if strings.HasPrefix(isoOffset, "UTC-") {
		return strings.Replace(isoOffset, "UTC-", "UTC+", 1)
	}
	return isoOffset
}

func appendEnviron(baseEnviron []string, name string, value string) []string {
	results := make([]string, 0)
	prefix := fmt.Sprintf("%s=", name)
	for _, v := range baseEnviron {
		if !strings.HasPrefix(v, prefix) {
			results = append(results, v)
		}
	}
	return append(results, fmt.Sprintf("%s=%s", name, value))
}