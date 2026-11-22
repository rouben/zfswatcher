package main

const VERSION = "0.5.0"

// buildVersion is the version shown by "zfswatcher -v" and on the about page.
// Development builds override it with -ldflags "-X main.buildVersion=...".
var buildVersion = VERSION
