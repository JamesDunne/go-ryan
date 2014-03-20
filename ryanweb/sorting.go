package main

import "os"

// For directory entry sorting:

type Entries []os.FileInfo

func (s Entries) Len() int      { return len(s) }
func (s Entries) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

type sortBy int

const (
	sortByName sortBy = iota
	sortByDate
	sortBySize
)

type sortDirection int

const (
	sortAscending sortDirection = iota
	sortDescending
)

// Sort by last modified time:
type ByDate struct {
	Entries
	dir sortDirection
}

func (s ByDate) Less(i, j int) bool {
	if s.Entries[i].IsDir() && !s.Entries[j].IsDir() {
		return true
	}
	if !s.Entries[i].IsDir() && s.Entries[j].IsDir() {
		return false
	}

	if s.dir == sortAscending {
		if s.Entries[i].ModTime().Equal(s.Entries[j].ModTime()) {
			return s.Entries[i].Name() > s.Entries[j].Name()
		} else {
			return s.Entries[i].ModTime().Before(s.Entries[j].ModTime())
		}
	} else {
		if s.Entries[i].ModTime().Equal(s.Entries[j].ModTime()) {
			return s.Entries[i].Name() > s.Entries[j].Name()
		} else {
			return s.Entries[i].ModTime().After(s.Entries[j].ModTime())
		}
	}
}
