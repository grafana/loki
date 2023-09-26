package api

import "time"

type GetSessionTokenResult struct {
	AccessKeyId     string
	SecretAccessKey string
	SessionToken    string
	CreateTime      string
	Expiration      string
	UserId          string
}

type AssumeRoleArgs struct {
	DurationSeconds int
	AccountId       string
	UserId          string
	RoleName        string
	Acl             string
}

type Credential struct {
	AccessKeyId     string
	SecretAccessKey string
	SessionToken    string
	CreateTime      time.Time
	Expiration      time.Time
	UserId          string
	RoleId          string
}
