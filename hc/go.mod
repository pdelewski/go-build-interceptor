module github.com/pdelewski/go-build-interceptor/hc

go 1.24.0

require golang.org/x/tools v0.39.0

require (
	golang.org/x/mod v0.30.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
)

replace github.com/pdelewski/go-build-interceptor/hooks => ../hooks
