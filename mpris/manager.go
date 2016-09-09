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
	"dbus/com/deepin/daemon/audio"
	"dbus/com/deepin/daemon/display"
	"dbus/com/deepin/daemon/helper/backlight"
	"dbus/com/deepin/daemon/keybinding"
	"dbus/com/deepin/sessionmanager"
	"dbus/org/freedesktop/dbus"
	"dbus/org/freedesktop/login1"
	"fmt"
	"pkg.deepin.io/lib/log"
	//"time"
)

var logger = log.NewLogger("daemon/mpris")

type Manager struct {
	mediakey       *keybinding.Mediakey
	login          *login1.Manager
	disp           *display.Display
	dbusDaemon     *dbus.DBusDaemon
	audioDaemon    *audio.Audio
	sessionManager *sessionmanager.SessionManager
	blDaemon       *backlight.Backlight

	prevPlayer string
}

func NewManager() (*Manager, error) {
	var m = new(Manager)

	var err error
	m.mediakey, err = keybinding.NewMediakey("com.deepin.daemon.Keybinding",
		"/com/deepin/daemon/Keybinding/Mediakey")
	if err != nil {
		return nil, err
	}

	m.login, err = login1.NewManager("org.freedesktop.login1",
		"/org/freedesktop/login1")
	if err != nil {
		return nil, err
	}

	m.dbusDaemon, err = dbus.NewDBusDaemon("org.freedesktop.DBus", "/")
	if err != nil {
		return nil, err
	}

	m.disp, err = display.NewDisplay("com.deepin.daemon.Display",
		"/com/deepin/daemon/Display")
	if err != nil {
		logger.Warning("Create display connection failed:", err)
	}

	m.audioDaemon, err = audio.NewAudio("com.deepin.daemon.Audio",
		"/com/deepin/daemon/Audio")
	if err != nil {
		logger.Warning("Create audio connection failed:", err)
	}

	m.sessionManager, err = sessionmanager.NewSessionManager("com.deepin.SessionManager",
		"/com/deepin/SessionManager")
	if err != nil {
		logger.Warning("Create session manager connection failed:", err)
	}

	m.blDaemon, err = backlight.NewBacklight("com.deepin.daemon.helper.Backlight",
		"/com/deepin/daemon/helper/Backlight")
	if err != nil {
		logger.Warning("Create backlight manager connection failed:", err)
	}

	return m, nil
}

func (m *Manager) destroy() {
	keybinding.DestroyMediakey(m.mediakey)
	login1.DestroyManager(m.login)
}

func (m *Manager) changeBrightness(raised, pressed bool) {
	if m.disp == nil || !pressed {
		return
	}

	values := m.disp.Brightness.Get()
	var step float64 = 0.05
	if !raised {
		step = -0.05
	}

	var backlightValid bool = true
	cur, max, err := m.getBacklightInfo()
	if err != nil {
		backlightValid = false
	}
	for output, v := range values {
		var discrete float64
		ret, _ := m.disp.QueryOutputFeature(output)
		if backlightValid && (ret == 1) {
			//if driverSupportedHotkey() {
			//	logger.Debug("[changeBrightness] driver supported, wait 200ms...")
			//	time.Sleep(time.Microsecond * 200)
			//	tmp, _, err := m.getBacklightInfo()
			//	if err != nil {
			//		logger.Warning("Get backlight info failed:", output, err)
			//		continue
			//	}
			//	logger.Debug("[changeBrightness] after driver:", cur, tmp, max)
			//	cur = tmp
			//}
			v = float64(cur) / float64(max)
		}
		discrete = v + step
		if discrete > 1.0 {
			discrete = 1
		}
		if discrete < 0.02 {
			discrete = 0.02
		}
		logger.Debug("[changeBrightness] will set to:", output, discrete)
		err1 := m.disp.SetBrightness(output, discrete)
		if err1 != nil {
			logger.Warning("[changeBrightness] set failed:", output, discrete, err1)
		}
	}

	// Show osd
	var signal = "BrightnessUp"
	if !raised {
		signal = "BrightnessDown"
	}
	logger.Debug("[changeBrightness] emit osd signal:", signal)
	go showOSD(signal)
}

func (m *Manager) setMute(pressed bool) {
	if !pressed {
		return
	}

	sink, err := m.getDefaultSink()
	if err != nil {
		logger.Warning("[GetDefaultSink] failed:", err)
		return
	}
	sink.SetMute(!sink.Mute.Get())
}

func (m *Manager) changeVolume(raised, pressed bool) {
	if m.audioDaemon == nil || !pressed {
		return
	}

	sink, err := m.getDefaultSink()
	if err != nil {
		logger.Warning("[GetDefaultSink] failed:", err)
		return
	}

	v := sink.Volume.Get()
	var step float64 = 0.05
	if !raised {
		step = -0.05
	}

	logger.Debug("[changeVolume] old sink info:", sink.Name.Get(), v)
	v += step
	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1.0
	}

	logger.Debug("[changeVolume] will set volume to:", v)
	if sink.Mute.Get() {
		sink.SetMute(false)
	}
	sink.SetVolume(v, true)

	// Show osd
	var signal = "AudioUp"
	if !raised {
		signal = "AudioDown"
	}

	logger.Debug("[changeVolume] emit signal:", signal)
	go showOSD(signal)
}

func (m *Manager) getDefaultSink() (*audio.AudioSink, error) {
	if m.audioDaemon == nil {
		return nil, fmt.Errorf("Can not connect audio daemon")
	}

	sinkPath, err := m.audioDaemon.GetDefaultSink()
	if err != nil {
		return nil, err
	}

	sink, err := audio.NewAudioSink("com.deepin.daemon.Audio", sinkPath)
	if err != nil {
		return nil, err
	}

	return sink, nil
}

func (m *Manager) suspend(pressed bool) {
	if !pressed {
		return
	}
	if m.sessionManager == nil {
		logger.Warning("can not connect session manager")
		return
	}
	var err error
	err = m.sessionManager.RequestSuspend()
	logger.Debug("Request suspend")
	if err != nil {
		logger.Warning("Request suspend failed: ", err)
	}
}

func (m *Manager) eject(pressed bool) {
	if !pressed {
		return
	}
	// eject CDROM
	doAction("eject -r")
}

func (m *Manager) changeKbdBrightness(raised, pressed bool) {
	if m.blDaemon == nil {
		return
	}

	value, err := m.blDaemon.GetKbdBrightness()
	if err != nil {
		logger.Debug("Query keyboard brightness failed:", err)
		return
	}

	maxValue, err := m.blDaemon.GetKbdMaxBrightness()
	if err != nil {
		logger.Debug("Query keyboard brightness failed:", err)
		return
	}

	logger.Debug("[changeKbdBrightness] pld kbd backlight info:", value, maxValue)
	if raised {
		value += 1
	} else {
		value -= 1
	}

	if value < 0 {
		value = 0
	} else if value > maxValue {
		value = maxValue
	}

	logger.Debug("[changeKbdBrightness] will set kbd backlight to:", value)
	err = m.blDaemon.SetKbdBrightness(value)
	if err != nil {
		logger.Warning("Set keyboard brightness failed:", value, err)
	}
}

func (m *Manager) getBacklightInfo() (int32, int32, error) {
	if m.blDaemon == nil {
		return 0, 0, fmt.Errorf("No backlight helper found")
	}

	list, _ := m.blDaemon.ListSysPath()
	if len(list) == 0 {
		return 0, 0, fmt.Errorf("No backlight helper found")
	}

	cur, err := m.blDaemon.GetBrightness(list[0])
	if err != nil {
		return 0, 0, err
	}

	max, err := m.blDaemon.GetMaxBrightness(list[0])
	if err != nil {
		return 0, 0, err
	}
	return cur, max, nil
}