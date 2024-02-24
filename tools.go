//go:build tools
// +build tools

package tools

import (
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/github"
	_ "github.com/golang/mock/mockgen"
	_ "github.com/golang/mock/mockgen/model"
)
