// Package samplefiles contains the immutable response fixtures shipped with httptoy.
package samplefiles

import "embed"

// Files makes fixtures available independently of the process working directory.
//
//go:embed data/* images/*
var Files embed.FS
