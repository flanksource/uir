module github.com/oipa/cli

go 1.22

require (
	github.com/quasilyte/go-ruleguard/dsl v0.3.23
	gorm.io/gorm v0.0.0
)

replace gorm.io/gorm => ./stub/gorm
