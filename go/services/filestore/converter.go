package filestore

import (
	"time"
)

// --- FileConverter (1:1 with app/js/FileConverter.js) -----------------------

type fseConverter struct {
	converter string
	prefix    []string
	enable    bool
}

var fseApprovedFormats = map[string]bool{"png": true, "jpg": true}

func (c *fseConverter) run(sourcePath, requestedFormat string, command []string) (string, error) {
	if !c.enable {
		return "", fseConvDisabled()
	}
	if !fseApprovedFormats[requestedFormat] {
		return "", fseConversion("invalid format requested")
	}
	destPath := sourcePath + "." + requestedFormat
	if c.converter == "pdftocairo" {
		command = append(command, sourcePath)
	} else {
		command = append(command, destPath)
	}
	full := append(append([]string{}, c.prefix...), command...)
	if _, err := fseExec(full, 40*time.Second); err != nil {
		return "", fseConversion("something went wrong converting file")
	}
	return destPath, nil
}

func (c *fseConverter) convert(sourcePath, requestedFormat string) (string, error) {
	if c.converter == "pdftocairo" {
		return c.run(sourcePath, requestedFormat, []string{
			"pdftocairo", "-png", "-singlefile", "-scale-to-x", "1500", "-scale-to-y", "-1", sourcePath,
		})
	}
	return c.run(sourcePath, requestedFormat, []string{
		"convert", "-define", "pdf:fit-page=600x", "-flatten", "-density", "300", sourcePath + "[0]",
	})
}

func (c *fseConverter) thumbnail(sourcePath string) (string, error) {
	if c.converter == "pdftocairo" {
		return c.run(sourcePath, "png", []string{
			"pdftocairo", "-png", "-singlefile", "-scale-to-x", "700", "-scale-to-y", "-1", sourcePath,
		})
	}
	return c.run(sourcePath, "png", []string{
		"convert", "-flatten", "-background", "white", "-density", "300", "-define", "pdf:fit-page=260x", sourcePath + "[0]", "-resize", "260x",
	})
}

func (c *fseConverter) preview(sourcePath string) (string, error) {
	if c.converter == "pdftocairo" {
		return c.run(sourcePath, "png", []string{
			"pdftocairo", "-png", "-singlefile", "-scale-to-x", "1000", "-scale-to-y", "-1", sourcePath,
		})
	}
	return c.run(sourcePath, "png", []string{
		"convert", "-flatten", "-background", "white", "-density", "300", "-define", "pdf:fit-page=1000x", sourcePath + "[0]", "-resize", "1000x",
	})
}
