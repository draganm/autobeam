package interpolatemanifests

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/draganm/manifestor/interpolate"
	"github.com/go-git/go-billy/v5"
	"gopkg.in/yaml.v3"
)

func RollOut(
	templatesPath string,
	values map[string]any,
	destFS billy.Filesystem,
) error {

	templatesPath, err := filepath.Abs(templatesPath)
	if err != nil {
		return fmt.Errorf("could not get absolute path for the deployment templates: %w", err)
	}

	templates := map[string][]byte{}

	allDirs := []string{}

	err = filepath.WalkDir(templatesPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			relativePath, err := filepath.Rel(templatesPath, path)
			if err != nil {
				return fmt.Errorf("could not get relative path of %s: %w", path, err)
			}

			if relativePath != "." {
				allDirs = append(allDirs, relativePath)
			}
		}

		if !d.Type().IsRegular() {
			return nil
		}

		ext := filepath.Ext(path)
		if !(ext == ".yaml" || ext == ".yml") {
			return nil
		}

		relativePath, err := filepath.Rel(templatesPath, path)
		if err != nil {
			return fmt.Errorf("could not get relative path of %s: %w", path, err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("could not read %s: %w", path, err)
		}

		templates[relativePath] = data

		return nil
	})

	if err != nil {
		return fmt.Errorf("could not read templates: %w", err)
	}

	pathExists := func(p string) (bool, error) {
		_, err := destFS.Stat(p)
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}

	for n, d := range templates {
		manifestPath := n

		exists, err := pathExists(manifestPath)
		if err != nil {
			return fmt.Errorf("could not check if %s exists: %w", manifestPath, err)
		}

		if !exists {
			err := destFS.MkdirAll(manifestPath, 0777)
			if err != nil {
				return fmt.Errorf("could not mkdir %s: %w", path.Dir(manifestPath), err)
			}
		}

		f, err := destFS.OpenFile(manifestPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
		if err != nil {
			return fmt.Errorf("could not open manifest output file %s: %w", manifestPath, err)
		}

		enc := yaml.NewEncoder(f)
		err = interpolate.Interpolate(string(d), "", values, enc)
		if err != nil {
			f.Close()
			return fmt.Errorf("could not interpolate %s: %w", manifestPath, err)
		}

		err = f.Close()
		if err != nil {
			return fmt.Errorf("could not close %s: %w", manifestPath, err)
		}
	}

	return nil

}
