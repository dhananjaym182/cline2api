package main

import "time"

// appVersion 是运行时显示的版本号。
// 构建时通过 -ldflags "-X main.appVersion=<version>" 注入（如 v1.3.0），
// 未注入时显示 "dev"。
// 各构建入口（desktop/build.sh、CI build.yml、Dockerfile）都会注入。
var appVersion = "dev"

// appGitCommit / appBuildDate 由构建脚本通过 -ldflags 注入，用于「关于」页的构建信息。
var (
	appGitCommit = ""
	appBuildDate = ""
)

// serverStartedAt 记录进程启动时间（包初始化即视为启动），用于「关于」页运行时长。
var serverStartedAt = time.Now()
