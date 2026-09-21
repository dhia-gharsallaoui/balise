package store

import "time"

// Change is one file write within an atomic commit. A Change with Delete set removes Path
// from the store instead of writing Data, so a caller that supersedes a page — the importer
// rewriting a page into a different scope's directory, say — can stage the removal of the
// path it replaced in the same commit as the replacement. Without it nothing in this system
// could ever delete a page, and every correction left the superseded copy on disk and in the
// index forever.
type Change struct {
	Path   string
	Data   []byte
	Delete bool
}

// Commit is one entry from a file's history.
type Commit struct {
	SHA     string
	Author  string
	When    time.Time
	Message string
}

// PageStore is the system of record for pages. Version is an opaque token — for the git
// implementation it is the blob SHA — used for optimistic concurrency on Write.
// 04 section 5 defers an S3 implementation to P2; this interface is what makes it swappable.
//
// List and Read both reflect the working tree, not HEAD: pages are edited outside this
// process, and reindex rebuilds the database from what is on disk, so an implementation
// must enumerate and read files whether or not they have been committed yet.
type PageStore interface {
	Read(path string) (data []byte, version string, err error)
	Write(path string, data []byte, ifVersion string) (string, error)
	List(prefix string) ([]string, error)
	Commit(changes []Change, author, message string) (string, error)
	History(path string, limit int) ([]Commit, error)
}
