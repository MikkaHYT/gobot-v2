package helpers

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	ErrBinaryUnavailable      = errors.New("binary is unavailable")
	ErrBinaryPermissionDenied = errors.New("binary permission denied")
)

type binarySpec struct {
	name   string
	envVar string
}

type BinaryCheck struct {
	Name            string
	EnvVar          string
	Path            string
	Source          string
	Err             error
	ConfiguredPath  string
	ConfiguredError error
}

var (
	ffmpegSpec  = binarySpec{name: "ffmpeg", envVar: "FFMPEG_PATH"}
	ffprobeSpec = binarySpec{name: "ffprobe", envVar: "FFPROBE_PATH"}
	ytdlpSpec   = binarySpec{name: "yt-dlp", envVar: "YTDLP_PATH"}
)

// findBinary resolves an exe path using environment variables > system PATH > external directory.
func findBinary(spec binarySpec) string {
	path, _ := resolveBinary(spec)
	return path
}

func resolveBinary(spec binarySpec) (string, string) {
	if v := os.Getenv(spec.envVar); v != "" {
		if runtime.GOOS != "windows" && strings.HasSuffix(strings.ToLower(v), ".exe") {
			clean := strings.TrimSuffix(v, ".exe")
			clean = strings.TrimSuffix(clean, ".EXE")
			if _, err := os.Stat(clean); err == nil {
				return clean, spec.envVar
			}
			if p, err := exec.LookPath(clean); err == nil {
				return p, spec.envVar
			}
		} else if _, err := os.Stat(v); err == nil {
			return v, spec.envVar
		}
	}

	if p, err := exec.LookPath(spec.name); err == nil {
		return p, "PATH"
	}

	if runtime.GOOS == "windows" {
		if _, err := os.Stat(filepath.Join("external", spec.name+".exe")); err == nil {
			return filepath.Join("external", spec.name+".exe"), "external directory"
		}
		if _, err := os.Stat(spec.name + ".exe"); err == nil {
			return "./" + spec.name + ".exe", "working directory"
		}
	} else {
		if _, err := os.Stat(filepath.Join("external", spec.name)); err == nil {
			return filepath.Join("external", spec.name), "external directory"
		}
		if _, err := os.Stat("./" + spec.name); err == nil {
			return "./" + spec.name, "working directory"
		}
	}

	return spec.name, "unresolved"
}

func GetFFmpegPath() string { return findBinary(ffmpegSpec) }

func GetFFprobePath() string { return findBinary(ffprobeSpec) }

func GetYTDLPPath() string { return findBinary(ytdlpSpec) }

func CheckRequiredBinaries() []BinaryCheck {
	specs := []binarySpec{ffmpegSpec, ffprobeSpec, ytdlpSpec}
	checks := make([]BinaryCheck, 0, len(specs))
	for _, spec := range specs {
		path, source := resolveBinary(spec)
		check := BinaryCheck{
			Name:   spec.name,
			EnvVar: spec.envVar,
			Path:   path,
			Source: source,
			Err:    ValidateExecutable(path),
		}
		if configured := os.Getenv(spec.envVar); configured != "" {
			check.ConfiguredPath = configured
			if source != spec.envVar {
				check.ConfiguredError = ValidateExecutable(configured)
			}
		}
		checks = append(checks, check)
	}
	return checks
}

func ValidateExecutable(path string) error {
	if strings.TrimSpace(path) == "" {
		return ErrBinaryUnavailable
	}

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return ErrBinaryPermissionDenied
		}
		if _, lookPathErr := exec.LookPath(path); lookPathErr != nil {
			if errors.Is(lookPathErr, os.ErrPermission) {
				return ErrBinaryPermissionDenied
			}
			return ErrBinaryUnavailable
		}
		return nil
	}
	if info.IsDir() {
		return ErrBinaryUnavailable
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return ErrBinaryPermissionDenied
	}
	return nil
}
