package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"runtime"

	"github.com/blang/semver"
	"github.com/inconshreveable/go-update"
)

const locallyBuilt = "(Locally Built)"

// RunWithUpdateCheck checks if an update is due, checks if current version is outdated and performs update if needed
func (c *TGFConfig) RunWithUpdateCheck() int {
	app := c.tgf
	const autoUpdateFile = "TGFAutoUpdate"

	if app.AutoUpdateSet {
		if app.AutoUpdate {
			app.Debug("Auto update is forced. Checking version...")
		} else {
			app.Debug("Auto update is force disabled. Bypassing update version check.")
			return c.Run()
		}
	} else {
		if !c.AutoUpdate {
			app.Debug("Auto update is disabled in the config. Bypassing update version check.")
			return c.Run()
		}
		if lastRefresh(autoUpdateFile) < c.AutoUpdateDelay {
			app.Debug("Less than %v since last check. Bypassing update version check.", c.AutoUpdateDelay.String)
			return c.Run()
		}
	}

	app.Debug("Comparing local and latest versions...")
	touchImageRefresh(autoUpdateFile)

	latestVersionString := c.UpdateVersion
	if latestVersionString == "" {
		fetchedVersion, err := getLatestVersion()
		if err != nil {
			printError("Error getting latest version: %v", err)
			return c.Run()
		}
		latestVersionString = fetchedVersion
	}

	latestVersion, err := semver.Make(latestVersionString)
	if err != nil {
		printError("Semver error on retrieved version %s: %v", latestVersionString, err)
		return c.Run()
	}

	if version != locallyBuilt {
		currentVersion, err := semver.Make(version)
		if err != nil {
			printWarning("Semver error on current version %s: %v", version, err)
			return c.Run()
		}

		if currentVersion.GTE(latestVersion) {
			app.Debug("Your current version (%v) is up to date.", currentVersion)
			return c.Run()
		}
	} else if !app.AutoUpdate {
		app.Debug("Currently running a locally built version, no update unless explicitly specified.")
		return c.Run()
	}

	url := getPlatformZipURL(latestVersion.String())

	executablePath, err := os.Executable()
	if err != nil {
		printError("Executable path error: %v", err)
	}

	printWarning("Updating %s from %s ==> %v", executablePath, version, latestVersion)
	if err := doUpdate(url); err != nil {
		printError("Failed update for %s: %v", url, err)
		return c.Run()
	}

	printWarning("TGF is restarting...")
	return c.restart()
}

func doUpdate(url string) (err error) {
	// check url
	if url == "" {
		return fmt.Errorf("Empty url")
	}

	// request the new zip file
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return
	}

	tgfFile, err := zipReader.File[0].Open()
	if err != nil {
		printError("Failed to read new version rollback from bad update: %v", err)
		return
	}

	err = update.Apply(tgfFile, update.Options{})
	if err != nil {
		if err := update.RollbackError(err); err != nil {
			printError("Failed to rollback from bad update: %v", err)
		}
	}
	return err
}

func getPlatformZipURL(version string) string {
	name := runtime.GOOS
	if name == "darwin" {
		name = "macOS"
	}
	return fmt.Sprintf("https://github.com/coveo/tgf/releases/download/v%[1]s/tgf_%[1]s_%[2]s_64-bits.zip", version, name)
}

func getLatestVersion() (string, error) {
	resp, err := http.Get("https://api.github.com/repos/coveooss/tgf/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var jsonResponse map[string]string
	json.NewDecoder(resp.Body).Decode(&jsonResponse)
	latestVersion := jsonResponse["tag_name"]
	if latestVersion == "" {
		return "", errors.New("Error parsing json response")
	}
	return latestVersion[1:], nil
}

// Restart re runs the app with all the arguments passed
func (c *TGFConfig) restart() int {
	cmd := exec.Command(os.Args[0], os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		printError("Error on restart: %v", err)
		return 1
	}
	return 0
}
