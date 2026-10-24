package main

import "cline-go-proxy/internal/domain"

// Temporary root compatibility aliases for the domain extraction. Call sites
// migrate to internal/domain; these aliases are removed once no package-main
// caller or local test depends on them.

type Account = domain.Account
type Model = domain.Model
type ModelStat = domain.ModelStat
type AccountPool = domain.AccountPool
type LoginMethod = domain.LoginMethod
type RequestLog = domain.RequestLog
type AttemptLog = domain.AttemptLog
type CustomProvider = domain.CustomProvider
type Connection = domain.Connection
type ModelCombo = domain.ModelCombo
type ComboEntry = domain.ComboEntry

const (
	MethodDeviceOAuth  = domain.MethodDeviceOAuth
	MethodRefreshToken = domain.MethodRefreshToken
	MethodSSOCookie    = domain.MethodSSOCookie
)
