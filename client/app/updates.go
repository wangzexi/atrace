package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
)

func init() {
	mygo.App.WhenReady(func() {
		// Default to background downloads on the first update-enabled launch, while
		// preserving subsequent choices made in MyGo's update preferences.
		if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "updater.json")); os.IsNotExist(err) {
				updater.SetAutomaticDownloads(true)
			}
		}
		updater.OnInstalled(func() { log.Print("Atrace update verified and staged; it takes effect on the next restart") })
	})
}
