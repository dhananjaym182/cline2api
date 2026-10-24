package main

import (
	"log"
	"net/http"
	"time"

	"cline-go-proxy/internal/adminapi"
)

var adminOAuth = &adminapi.OAuth{
	Store: rootAccountStore{},
	StartDevice: func() (adminapi.DeviceAuthorization, error) {
		d, err := workosDeviceAuth()
		if err != nil {
			return adminapi.DeviceAuthorization{}, err
		}
		return adminapi.DeviceAuthorization{
			DeviceCode:              d.DeviceCode,
			UserCode:                d.UserCode,
			VerificationURI:         d.VerificationURI,
			VerificationURIComplete: d.VerificationURIComplete,
			Interval:                d.Interval,
			ExpiresIn:               d.ExpiresIn,
		}, nil
	},
	PollToken: func(deviceCode string, interval, expiresIn int) (adminapi.OAuthToken, error) {
		tok, err := pollWorkosToken(deviceCode, interval, expiresIn)
		if err != nil {
			return adminapi.OAuthToken{}, err
		}
		return adminapi.OAuthToken{AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken}, nil
	},
	RegisterAccount: func(accessToken, refreshToken string) (adminapi.OAuthAccount, error) {
		resp, err := registerWithCline(accessToken, refreshToken)
		if err != nil {
			return adminapi.OAuthAccount{}, err
		}
		email := ""
		if resp.Data.UserInfo != nil {
			email = resp.Data.UserInfo.Email
		}
		return adminapi.OAuthAccount{
			Email:        email,
			AccessToken:  resp.Data.AccessToken,
			RefreshToken: resp.Data.RefreshToken,
			ExpiresAt:    parseExpiry(resp.Data.ExpiresAt),
		}, nil
	},
	Now:       time.Now,
	Translate: func(r *http.Request, key string) string { return tAPI(r, key) },
	Logf:      log.Printf,
}
