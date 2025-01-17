// SPDX-FileCopyrightText: 2018 - 2022 UnionTech Software Technology Co., Ltd.
//
// SPDX-License-Identifier: GPL-3.0-or-later

package system

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/linuxdeepin/go-lib/log"
	"github.com/linuxdeepin/go-lib/strv"
)

type MirrorSource struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	Url  string `json:"url"`

	NameLocale  map[string]string `json:"name_locale"`
	Weight      int               `json:"weight"`
	Country     string            `json:"country"`
	AdjustDelay int               `json:"adjust_delay"` // ms
}

var RepoInfos []RepositoryInfo
var logger = log.NewLogger("lastore")

type RepositoryInfo struct {
	Name   string `json:"name"`
	Url    string `json:"url"`
	Mirror string `json:"mirror"`
}

func DecodeJson(fpath string, d interface{}) error {
	// #nosec G304
	f, err := os.Open(fpath)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	return json.NewDecoder(f).Decode(&d)
}

func EncodeJson(fpath string, d interface{}) error {
	f, err := os.Create(fpath)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	return json.NewEncoder(f).Encode(d)
}

func NormalFileExists(fpath string) bool {
	info, err := os.Stat(fpath)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	return true
}

type UpdateType uint64

// 用于设置UpdateMode属性,最大支持64位
const (
	SystemUpdate       UpdateType = 1 << 0 // 系统更新
	AppStoreUpdate     UpdateType = 1 << 1 // 应用更新(1050版本中应用更新不开启)
	SecurityUpdate     UpdateType = 1 << 2 // 1050及以上版本,安全更新项废弃,改为仅安全更新,1060恢复使用
	UnknownUpdate      UpdateType = 1 << 3 // 未知来源更新
	OnlySecurityUpdate UpdateType = 1 << 4 // 仅开启安全更新（该选项开启时，其他更新关闭）
)

func (m UpdateType) JobType() string {
	switch m {
	case SystemUpdate:
		return SystemUpgradeJobType
	case AppStoreUpdate:
		return AppStoreUpgradeJobType
	case SecurityUpdate, OnlySecurityUpdate:
		return SecurityUpgradeJobType
	case UnknownUpdate:
		return UnknownUpgradeJobType
	default:
		return ""
	}
}

func AllUpdateType() []UpdateType {
	return []UpdateType{
		SystemUpdate,
		SecurityUpdate,
		//AppStoreUpdate,
		OnlySecurityUpdate,
		UnknownUpdate,
	}
}

const (
	LastoreSourcesPath = "/var/lib/lastore/sources.list"
	CustomSourceDir    = "/var/lib/lastore/sources.list.d"
	OriginSourceDir    = "/etc/apt/sources.list.d"
	SystemSourceFile   = "/etc/apt/sources.list"
	SystemSourceDir    = "/var/lib/lastore/SystemSource.d"
	AppStoreList       = "appstore.list"
	AppStoreSourceFile = "/etc/apt/sources.list.d/" + AppStoreList
	UnstableSourceList = "deepin-unstable-source.list"
	UnstableSourceFile = "/etc/apt/sources.list.d/" + UnstableSourceList
	HweSourceList      = "hwe.list"
	HweSourceFile      = "/etc/apt/sources.list.d/" + HweSourceList
	DriverList         = "driver.list"
	SecurityList       = "security.list"
	SecuritySourceFile = "/etc/apt/sources.list.d/" + SecurityList // 安全更新源路径
	UnknownSourceDir   = "/var/lib/lastore/unknownSource.d"        // 未知来源更新的源个数不定,需要创建软链接放在同一目录内
)

// GetCategorySourceMap 缺省更新类型与对应仓库的map
func GetCategorySourceMap() map[UpdateType]string {
	return map[UpdateType]string{
		SystemUpdate: SystemSourceDir,
		//AppStoreUpdate:     AppStoreSourceFile,
		SecurityUpdate:     SecuritySourceFile,
		OnlySecurityUpdate: SecuritySourceFile,
		UnknownUpdate:      UnknownSourceDir,
	}
}

func UpdateUnknownSourceDir(nonUnknownList strv.Strv) error {
	// 移除旧版本内容
	err := os.RemoveAll(CustomSourceDir)
	if err != nil {
		logger.Warning(err)
	}
	err = os.RemoveAll(LastoreSourcesPath)
	if err != nil {
		logger.Warning(err)
	}
	// 移除旧数据
	err = os.RemoveAll(UnknownSourceDir)
	if err != nil {
		logger.Warning(err)
	}
	// #nosec G301
	err = os.MkdirAll(UnknownSourceDir, 0755)
	if err != nil {
		logger.Warning(err)
	}

	var unknownSourceFilePaths []string
	sourceDirFileInfos, err := ioutil.ReadDir(OriginSourceDir)
	if err != nil {
		logger.Warning(err)
		return err
	}
	if len(nonUnknownList) == 0 {
		nonUnknownList = strv.Strv{
			AppStoreList,
			SecurityList,
			DriverList,
			UnstableSourceList,
			HweSourceList,
		}
	}
	for _, fileInfo := range sourceDirFileInfos {
		name := fileInfo.Name()
		if strings.HasSuffix(name, ".list") {
			if !nonUnknownList.Contains(name) {
				unknownSourceFilePaths = append(unknownSourceFilePaths, filepath.Join(OriginSourceDir, name))
			}
		}
	}

	// 创建对应的软链接
	for _, filePath := range unknownSourceFilePaths {
		linkPath := filepath.Join(UnknownSourceDir, filepath.Base(filePath))
		err = os.Symlink(filePath, linkPath)
		if err != nil {
			return fmt.Errorf("create symlink for %q failed: %v", filePath, err)
		}
	}
	return nil
}

func UpdateSystemSourceDir(systemSourceList []string) error {
	err := os.RemoveAll(SystemSourceDir)
	if err != nil {
		logger.Warning(err)
	}
	// #nosec G301
	err = os.MkdirAll(SystemSourceDir, 0755)
	if err != nil {
		logger.Warning(err)
	}
	if len(systemSourceList) == 0 {
		systemSourceList = []string{UnstableSourceFile, SystemSourceFile, HweSourceFile}
	}
	// 创建对应的软链接
	for _, filePath := range systemSourceList {
		linkPath := filepath.Join(SystemSourceDir, filepath.Base(filePath))
		err = os.Symlink(filePath, linkPath)
		if err != nil {
			return fmt.Errorf("create symlink for %q failed: %v", filePath, err)
		}
	}
	return nil
}

// CustomSourceWrapper 根据updateType组合source文件,doRealAction完成实际操作,unref用于释放资源
func CustomSourceWrapper(updateType UpdateType, doRealAction func(path string, unref func()) error) error {
	var sourcePathList []string
	for _, t := range AllUpdateType() {
		category := updateType & t
		if category != 0 {
			sourcePath := GetCategorySourceMap()[category]
			sourcePathList = append(sourcePathList, sourcePath)
		}
	}
	if len(sourcePathList) == 1 {
		// 如果只有一个仓库，证明是单项的更新，可以直接使用默认的文件夹
		if doRealAction != nil {
			return doRealAction(GetCategorySourceMap()[updateType], nil)
		}
		return errors.New("doRealAction is nil")
	} else {
		// 仓库组合的情况，需要重新组合文件
		// #nosec G301
		sourceDir, err := ioutil.TempDir("/tmp", "*Source.d")
		if err != nil {
			logger.Warning(err)
		}
		var allSourceFilePaths []string
		for _, path := range sourcePathList {
			fileInfo, err := os.Stat(path)
			if err != nil {
				continue
			}
			if fileInfo.IsDir() {
				allSourceDirFileInfos, err := ioutil.ReadDir(path)
				if err != nil {
					continue
				}
				for _, fileInfo := range allSourceDirFileInfos {
					name := fileInfo.Name()
					if strings.HasSuffix(name, ".list") {
						allSourceFilePaths = append(allSourceFilePaths, filepath.Join(path, name))
					}
				}
			} else {
				allSourceFilePaths = append(allSourceFilePaths, path)
			}
		}

		// 创建对应的软链接
		for _, filePath := range allSourceFilePaths {
			linkPath := filepath.Join(sourceDir, filepath.Base(filePath))
			err := os.Symlink(filePath, linkPath)
			if err != nil {
				return fmt.Errorf("create symlink for %q failed: %v", filePath, err)
			}
		}
		unref := func() {
			err := os.RemoveAll(sourceDir)
			if err != nil {
				logger.Warning(err)
			}
		}
		if doRealAction != nil {
			return doRealAction(sourceDir, unref)
		}
		return errors.New("doRealAction is nil")
	}
}

type UpgradeStatus string

const (
	UpgradeReady   UpgradeStatus = "ready"
	UpgradeRunning UpgradeStatus = "running"
	UpgradeFailed  UpgradeStatus = "failed"
)

type UpgradeReasonCode uint

const ( // TODO 定义错误码（发通知时使用）
	NoError UpgradeReasonCode = iota
	ErrorCode1
	ErrorCode2
)

type UpgradeStatusAndReason struct {
	Status     UpgradeStatus
	ReasonCode UpgradeReasonCode
}

func GetGrubRollbackTitle(grubPath string) (string, error) {
	var rollbackTitle string
	fileContent, err := ioutil.ReadFile(grubPath)
	if err != nil {
		logger.Warning(err)
		return "", err
	}
	sl := bufio.NewScanner(strings.NewReader(string(fileContent)))
	sl.Split(bufio.ScanLines)
	needNext := false
	for sl.Scan() {
		line := sl.Text()
		line = strings.TrimSpace(line)
		if !needNext {
			needNext = strings.Contains(line, "BEGIN /etc/grub.d/11_deepin_ab_recovery")
		} else {
			if strings.HasPrefix(line, "menuentry ") {
				title, ok := parseTitle(line)
				if ok {
					rollbackTitle = title
				} else {
					err := fmt.Errorf("parse entry title failed from: %q", line)
					return "", err
				}
			}
			break
		}
	}
	err = sl.Err()
	if err != nil {
		return "", err
	}
	return rollbackTitle, nil
}

var (
	entryRegexpSingleQuote = regexp.MustCompile(`^ *(menuentry|submenu) +'(.*?)'.*$`)
	entryRegexpDoubleQuote = regexp.MustCompile(`^ *(menuentry|submenu) +"(.*?)".*$`)
)

func parseTitle(line string) (string, bool) {
	line = strings.TrimLeftFunc(line, unicode.IsSpace)
	if entryRegexpSingleQuote.MatchString(line) {
		return entryRegexpSingleQuote.FindStringSubmatch(line)[2], true
	} else if entryRegexpDoubleQuote.MatchString(line) {
		return entryRegexpDoubleQuote.FindStringSubmatch(line)[2], true
	} else {
		return "", false
	}
}

func HandleDelayPackage(hold bool, packages []string) {
	action := "unhold"
	if hold {
		action = "hold"
	}
	args := []string{
		action,
	}
	args = append(args, packages...)
	err := exec.Command("apt-mark", args...).Run()
	if err != nil {
		logger.Warning(err)
	}
}
