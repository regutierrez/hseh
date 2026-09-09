// Package lockfile serialises hseh processes with an exclusive flock on a
// path, retrying while another process holds it.
package lockfile
