package version

import "fmt"

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func String() string {
	return fmt.Sprintf("planmaxx version %s\ncommit %s\ndate %s\n", Version, Commit, Date)
}
