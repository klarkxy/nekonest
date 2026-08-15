// Package buildinfo contains the daemon release identity.
package buildinfo

// Version is the NekoNest application release reported to the server. Release
// builds may override it with -ldflags "-X github.com/nekonest/daemon/internal/buildinfo.Version=X.Y.Z".
var Version = "0.2.8-rc.1"
