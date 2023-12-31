package licensing

import (
	"errors"
	"io/fs"
	"os"

	"github.com/khulnasoft-lab/fanal/licensing/classification"
	"github.com/khulnasoft-lab/fanal/licensing/config"
	"github.com/khulnasoft-lab/fanal/log"
	"github.com/khulnasoft-lab/fanal/types"
	"golang.org/x/xerrors"
	"gopkg.in/yaml.v3"
)

type Scanner struct {
	classifier *classification.Classifier
}

var defaultConfig = config.Config{
	// set the confidence threshold to an arbitrary 70% confidence
	MatchConfidenceThreshold: 0.7,

	// check the headers of human readable source code files for headers by default
	IncludeHeaders: true,

	// license acceptable risk threshold - default to 4 (restricted and above)
	RiskThreshold: 4,

	// ignore the following licenses
	IgnoredLicences: []string{},
}

type ScanArgs struct {
	FilePath string
	Content  []byte
}

func NewScanner(configPath string) (Scanner, error) {

	if configPath == "" {
		return newDefaultScanner()
	}

	f, err := os.Open(configPath)
	if errors.Is(err, os.ErrNotExist) {
		log.Logger.Debugf("No license config detected: %s", configPath)
		return newDefaultScanner()
	} else if err != nil {

		return Scanner{}, xerrors.Errorf("file open error %s: %w", configPath, err)
	}
	defer func() { _ = f.Close() }()

	log.Logger.Infof("Loading %s for license scanning...", configPath)

	var cfg config.Config
	if err = yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return Scanner{}, xerrors.Errorf("license config decode error: %w", err)
	}

	classifier, err := classification.NewClassifier(cfg)
	if err != nil {
		return Scanner{}, xerrors.Errorf("classifier could not be created: %w", err)
	}
	return Scanner{classifier: classifier}, nil
}

func newDefaultScanner() (Scanner, error) {
	classifier, err := classification.NewClassifier(defaultConfig)
	if err != nil {
		return Scanner{}, xerrors.Errorf("classifier could not be created: %w", err)
	}
	return Scanner{classifier: classifier}, nil
}

func (s Scanner) ScanFS(filesystem fs.FS) ([]types.LicenseFile, error) {

	var licenseFiles []types.LicenseFile

	err := fs.WalkDir(filesystem, ".", func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		content, err := fs.ReadFile(filesystem, path)
		licenseFile, err := s.classifier.Classify(path, content)
		if err != nil {
			return err
		}

		if len(licenseFile.Findings) > 0 {
			if err := findPackage(path, filesystem, &licenseFile); err != nil {
				log.Logger.Errorf("an error occurred while finding package for file %s: %w", path, err)
			}
			licenseFiles = append(licenseFiles, licenseFile)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return licenseFiles, nil
}

func (s Scanner) Scan(scanArgs ScanArgs) types.LicenseFile {

	license, err := s.classifier.Classify(scanArgs.FilePath, scanArgs.Content)
	if err != nil {
		log.Logger.Debugf("Name scan failed while scanning %s: %w", scanArgs.FilePath, err)
	}

	return license
}
