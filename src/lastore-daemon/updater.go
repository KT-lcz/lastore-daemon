/**
 * Copyright (C) 2015 Deepin Technology Co., Ltd.
 *
 * This program is free software; you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation; either version 3 of the License, or
 * (at your option) any later version.
 **/

package main

import (
	"fmt"
	log "github.com/cihub/seelog"
	"internal/system"
	"io/ioutil"
	"os/exec"
	"path"
	"pkg.deepin.io/lib/dbus"
)

type ApplicationUpdateInfo struct {
	Id             string
	Name           string
	Icon           string
	CurrentVersion string
	LastVersion    string

	// There  hasn't support
	changeLog string
}

type Updater struct {
	AutoCheckUpdates bool

	MirrorSource string

	b      system.System
	config *Config

	UpdatableApps     []string
	UpdatablePackages []string
}

func NewUpdater(b system.System, config *Config) *Updater {
	u := &Updater{
		b:                b,
		config:           config,
		AutoCheckUpdates: config.AutoCheckUpdates,
		MirrorSource:     config.MirrorSource,
	}

	if u.AutoCheckUpdates {
		go exec.Command("systemctl start lastore-update-metadata-info.timer").Run()
	}

	dm := system.NewDirMonitor(system.VarLibDir)

	dm.Add(func(fpath string, op uint32) {
		u.loadUpdateInfos()
	}, "update_infos.json", "package_icons.json", "applications.json")

	err := dm.Start()
	if err != nil {
		log.Warnf("Can't create inotify on %s: %v\n", system.VarLibDir, err)
	}

	u.loadUpdateInfos()

	return u
}

func SetAPTSmartMirror(url string) error {
	return ioutil.WriteFile("/etc/apt/apt.conf.d/99mirrors.conf",
		([]byte)(fmt.Sprintf("Acquire::SmartMirrors::MirrorSource %q;", url)),
		0644)
}

// 设置用于下载软件的镜像源
func (u *Updater) SetMirrorSource(id string) error {
	if u.MirrorSource == id {
		return nil
	}
	for _, m := range u.ListMirrorSources("") {
		if m.Id != id {
			continue
		}

		if m.Url == "" {
			return system.NotFoundError
		}
		if err := SetAPTSmartMirror(m.Url); err != nil {
			log.Warnf("SetMirrorSource(%q) failed:%v\n", id, err)
			return err
		}
	}

	err := u.config.SetMirrorSource(id)
	if err != nil {
		return err
	}
	u.MirrorSource = u.config.MirrorSource
	dbus.NotifyChange(u, "MirrorSource")
	return nil
}

type LocaleMirrorSource struct {
	Id   string
	Url  string
	Name string
}

// ListMirrors 返回当前支持的镜像源列表．顺序按优先级降序排
// 其中Name会根据传递进来的lang进行本地化
func (u Updater) ListMirrorSources(lang string) []LocaleMirrorSource {
	var raws []system.MirrorSource
	system.DecodeJson(path.Join(system.VarLibDir, "mirrors.json"), &raws)

	makeLocaleMirror := func(lang string, m system.MirrorSource) LocaleMirrorSource {
		ms := LocaleMirrorSource{
			Id:   m.Id,
			Url:  m.Url,
			Name: m.Name,
		}
		if v, ok := m.NameLocale[lang]; ok {
			ms.Name = v
		}
		return ms
	}

	var r []LocaleMirrorSource
	for _, raw := range raws {
		if raw.Weight < 0 {
			continue
		}
		r = append(r, makeLocaleMirror(lang, raw))
	}

	return r
}

func UpdatableNames(infos []system.UpgradeInfo) []string {
	var apps []string
	for _, info := range infos {
		apps = append(apps, info.Package)
	}
	return apps
}

func (u *Updater) SetAutoCheckUpdates(enable bool) error {
	if u.AutoCheckUpdates == enable {
		return nil
	}

	u.AutoCheckUpdates = enable
	dbus.NotifyChange(u, "AutoCheckUpdates")

	if enable {
		return exec.Command("systemctl", "unmask", "lastore-update-metadata-info.timer").Run()
	} else {
		return exec.Command("systemctl", "mask", "lastore-update-metadata-info.timer").Run()
	}
}