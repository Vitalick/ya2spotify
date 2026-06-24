module ya2spotify

go 1.25.0

require (
	github.com/google/uuid v1.6.0
	github.com/sirupsen/logrus v1.9.4
	github.com/stretchr/testify v1.10.0
	github.com/zmb3/spotify/v2 v2.4.3
	golang.org/x/text v0.38.0
)

replace github.com/zmb3/spotify/v2 => ../spotify

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
