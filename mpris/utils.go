/**
 * Copyright (C) 2014 Deepin Technology Co., Ltd.
 *
 * This program is free software; you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation; either version 3 of the License, or
 * (at your option) any later version.
 **/

package mpris

import (
	"fmt"
	"gir/gio-2.0"
	"os/exec"

	"io/ioutil"
	"pkg.deepin.io/lib/dbus"
	"strings"
)

const (
	mimeTypeBrowser    = "x-scheme-handler/http"
	mimeTypeEmail      = "x-scheme-handler/mailto"
	mimeTypeCalc       = "x-scheme-handler/calculator"
	mimeTypeAudioMedia = "audio/mpeg"
)

const (
	ibmHotkeyFile = "/proc/acpi/ibm/hotkey"
)

func execByMime(mime string, pressed bool) error {
	if !pressed {
		return nil
	}

	cmd := queryCommand(mime)
	if len(cmd) == 0 {
		return fmt.Errorf("Not found executable for: %s", mime)
	}
	return doAction(cmd)
}

func showOSD(signal string) {
	sessionDBus, _ := dbus.SessionBus()
	sessionDBus.Object("com.deepin.dde.osd", "/").Call("com.deepin.dde.osd.ShowOSD", 0, signal)
}

func queryCommand(mime string) string {
	if mime == mimeTypeCalc {
		return "gnome-calculator"
	}

	app := gio.AppInfoGetDefaultForType(mime, false)
	if app == nil {
		return ""
	}
	defer app.Unref()

	return app.GetExecutable()
}

func doAction(cmd string) error {
	logger.Debug("execute command: ", cmd)
	return exec.Command("/bin/sh", "-c", cmd).Run()
}

var driverSupportedHotkey = func() func() bool {
	var (
		init      bool = false
		supported bool = false
	)

	return func() bool {
		if !init {
			init = true
			supported = checkIBMHotkey(ibmHotkeyFile)
		}
		return supported
	}
}()

func checkIBMHotkey(file string) bool {
	content, err := ioutil.ReadFile(file)
	if err != nil {
		return false
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		list := strings.Split(line, ":")
		if len(list) != 2 {
			continue
		}

		if list[0] != "status" {
			continue
		}

		if strings.TrimSpace(list[1]) == "enabled" {
			return true
		}
	}
	return false
}
