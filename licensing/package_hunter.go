package licensing

import (
	"io/fs"
	"path/filepath"

	"github.com/khulnasoft-lab/fanal/log"
	"github.com/khulnasoft-lab/fanal/types"
)

var LicensePackageFiles = []string{
	types.NuGetPkgsLock,
	types.NuGetPkgsConfig,
	types.GoMod,
	types.GoSum,
	types.MavenPom,
	types.NpmPkgLock,
	types.YarnLock,
	types.ComposerLock,
	types.PipRequirements,
	types.PipfileLock,
	types.PoetryLock,
	types.GemfileLock,
	types.CargoLock,
}

func findPackage(path string, filesystem fs.FS, licenseFile *types.LicenseFile) error {
	log.Logger.Debugf("looking for parent package of license file: %s", path)
	siblingPath := filepath.Dir(path)
	return fs.WalkDir(filesystem, siblingPath, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		for _, fileType := range LicensePackageFiles {
			if info.Name() == fileType {
				licenseFile.Package = filepath.Base(filepath.Dir(path))
				break
			}
		}

		return nil
	})
}
