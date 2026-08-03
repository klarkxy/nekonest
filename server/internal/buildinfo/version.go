// Package buildinfo contains the server release identity.
package buildinfo

// Version is the NekoNest application release reported to clients. Release
// builds may override it with -ldflags "-X github.com/nekonest/server/internal/buildinfo.Version=X.Y.Z".
var Version = "0.2.1"
