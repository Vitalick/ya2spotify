module ya2spotify

go 1.25.0

require (
	github.com/antchfx/htmlquery v1.3.6
	github.com/go-viper/mapstructure/v2 v2.4.0
	github.com/google/uuid v1.6.0
	github.com/sirupsen/logrus v1.9.4
	github.com/stretchr/testify v1.10.0
	github.com/zmb3/spotify/v2 v2.4.3
	golang.org/x/net v0.33.0
	golang.org/x/text v0.38.0
)

replace (
	github.com/zmb3/spotify/v2 => ../spotify
)

require (
	github.com/antchfx/xpath v1.3.6 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
