package main

// Version is the local-development fallback. Release builds inject the tag's
// semantic version with: -ldflags "-X main.Version=<version>".
var Version = "0.7.3"
