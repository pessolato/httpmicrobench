package osutil

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
)

type BuildCtxSpec struct {
	FineName string
	PathTo   string
	Mode     int64
}

func BuildGo(dest, mod string) error {
	cmd := exec.Command("go", "build", "-o", dest, mod)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("error to build %s with output %s and error: %w", mod, out, err)
	}
	return nil
}

func BuildCtx(specs ...BuildCtxSpec) (io.Reader, error) {
	if len(specs) < 1 {
		return nil, fmt.Errorf("cannot build context with no context specification")
	}

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	for _, s := range specs {
		err := FileToTar(s.FineName, s.PathTo, s.Mode, tw)
		if err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("error to build context: %w", err)
	}
	if err := gzw.Close(); err != nil {
		return nil, fmt.Errorf("error to compress context: %w", err)
	}

	return &buf, nil
}

func FileToTar(name, filePath string, mode int64, tw *tar.Writer) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("error to open file %s: %w", filePath, err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("error to get info on file %s: %w", filePath, err)
	}

	hdr, err := tar.FileInfoHeader(fi, "")
	if err != nil {
		return fmt.Errorf("error to create headers for file %s: %w", filePath, err)
	}
	hdr.Name = name
	hdr.Mode = mode

	err = tw.WriteHeader(hdr)
	if err != nil {
		return fmt.Errorf("error to write headers for file %s: %w", filePath, err)
	}
	_, err = io.Copy(tw, f)
	if err != nil {
		return fmt.Errorf("error to archive file %s: %w", filePath, err)
	}

	return nil
}
