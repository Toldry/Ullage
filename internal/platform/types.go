// Package platform is the only place where operating-system specifics live. Everything
// outside it works with the portable types declared here, so a future Linux port only
// needs another implementation of this package.
package platform

import (
	"errors"
	"fmt"
)

// EntryKind distinguishes the three kinds of directory entries the scanner cares about.
type EntryKind uint8

const (
	EntryFile EntryKind = iota
	EntryDirectory
	// EntryLink is any entry the scanner must not traverse: junctions, symbolic links,
	// and mount points of other volumes.
	EntryLink
)

// EntryAttributes is a portable subset of file attribute flags.
type EntryAttributes uint16

const (
	AttributeCompressed EntryAttributes = 1 << iota
	AttributeSparse
	// AttributeRecallOnDataAccess, AttributeRecallOnOpen and AttributeOffline mark cloud
	// placeholders whose content is not (fully) stored on this disk.
	AttributeRecallOnDataAccess
	AttributeRecallOnOpen
	AttributeOffline
	AttributeHidden
	AttributeSystem
)

// NotStoredLocally reports whether the entry is a cloud placeholder or offline file.
func (attributes EntryAttributes) NotStoredLocally() bool {
	const placeholderMask = AttributeRecallOnDataAccess | AttributeRecallOnOpen |
		AttributeOffline
	return attributes&placeholderMask != 0
}

// FileID identifies a file on one volume. Windows uses the volume serial number and the
// 128-bit NTFS/ReFS file ID; Linux would use st_dev and the inode number.
type FileID struct {
	Volume uint64
	High   uint64
	Low    uint64
}

// IsZero reports whether the identity is unknown.
func (identity FileID) IsZero() bool {
	return identity == FileID{}
}

// DirectoryEntry is one entry returned by a directory enumeration.
type DirectoryEntry struct {
	Name       string
	Kind       EntryKind
	Attributes EntryAttributes
	// AllocatedBytes is the space the entry occupies on disk, which for compressed and
	// sparse files is smaller than LogicalBytes.
	AllocatedBytes int64
	LogicalBytes   int64
	// ModifiedUnixNanos is the last write time in nanoseconds since the Unix epoch.
	ModifiedUnixNanos int64
	// CreatedUnixNanos is the creation (birth) time, or 0 when the file system does not
	// record one.
	CreatedUnixNanos int64
	ID               FileID
	// LinkCount is the number of hard links, or 0 when the enumeration cannot report it
	// cheaply (Windows). Callers must then assume the entry may be hard-linked.
	LinkCount uint32
	// LinkTarget is set for EntryLink entries when the target could be read.
	LinkTarget string
}

// DirectoryReader enumerates one directory. Implementations must not follow links.
type DirectoryReader interface {
	ReadDirectory(path string) ([]DirectoryEntry, error)
}

// RootInfo describes a scan root.
type RootInfo struct {
	ID                FileID
	FileSystem        string
	ModifiedUnixNanos int64
	CreatedUnixNanos  int64
}

// Volume describes a mounted volume the user can choose to scan.
type Volume struct {
	// RootPath is the native root path, for example `C:\` or `/`.
	RootPath    string
	DisplayName string
	Label       string
	FileSystem  string
	Kind        string
	TotalBytes  int64
	FreeBytes   int64
}

// UsedBytes is the space in use as the volume reports it.
func (volume Volume) UsedBytes() int64 {
	return volume.TotalBytes - volume.FreeBytes
}

// InstalledApplication is one entry from the operating system's list of installed programs.
type InstalledApplication struct {
	DisplayName     string
	Publisher       string
	InstallLocation string
	// EstimatedBytes comes from the installer and is informational only.
	EstimatedBytes int64
}

// KnownLocations holds the values that recipe and System-tier patterns expand from
// variables such as {SystemRoot} or {UserProfile}. Paths are native.
type KnownLocations struct {
	Variables map[string][]string
	// CaseInsensitive is true when the file system compares names case-insensitively.
	CaseInsensitive bool
	// PathSeparator is the native separator.
	PathSeparator byte
}

// ErrAccessDenied is wrapped by errors that a DirectoryReader returns when permissions
// prevent reading a directory.
var ErrAccessDenied = errors.New("access denied")

// AccessDeniedError attaches the path to ErrAccessDenied.
type AccessDeniedError struct {
	Path  string
	Cause error
}

func (accessError *AccessDeniedError) Error() string {
	return fmt.Sprintf("access denied: %s: %v", accessError.Path, accessError.Cause)
}

func (accessError *AccessDeniedError) Unwrap() []error {
	return []error{ErrAccessDenied, accessError.Cause}
}

// IsAccessDenied reports whether err means a permission problem.
func IsAccessDenied(err error) bool {
	return errors.Is(err, ErrAccessDenied)
}
