package glob

// Code modified from: https://github.com/bmatcuk/doublestar

// The MIT License (MIT)
//
// Copyright (c) 2014 Bob Matcuk
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ErrBadPattern represents a bad glob pattern.
var ErrBadPattern = path.ErrBadPattern

// Glob returns a map[string]os.FileInfo based on the provided glob. It will
// ignore any paths included in the ignores argument. When matching with a
// directory, if traverseDirs is true, it will traverse the directory, if it is
// false it will return the directory without traversing.
func Glob(matches map[string]os.FileInfo, pattern string, ignores []string, traverseDirs bool) (map[string]os.FileInfo, error) {
	patternComponents := splitPathOnSeparator(filepath.ToSlash(pattern), '/')
	if len(patternComponents) == 0 {
		return nil, nil
	}
	// instantiate map if it is nil
	if matches == nil {
		matches = make(map[string]os.FileInfo)
	}
	// On Windows systems, this will return the drive name ('C:'), on others,
	// it will return an empty string.
	volumeName := filepath.VolumeName(pattern)
	// If the first pattern component is equal to the volume name, then the
	// pattern is an absolute path.
	var err error
	if patternComponents[0] == volumeName {
		baseDir := fmt.Sprintf("%s%s", volumeName, string(os.PathSeparator))
		err = doGlob(matches, baseDir, patternComponents[1:], ignores, traverseDirs)
	} else {
		err = doGlob(matches, ".", patternComponents, ignores, traverseDirs)
	}
	if err != nil {
		return nil, err
	}
	return matches, nil
}

func doGlob(matches map[string]os.FileInfo, basedir string, components []string, ignores []string, traverseDirs bool) error {
	// figure out how many components we don't need to glob because they're
	// just straight directory names
	patLen := len(components)
	patIdx := 0
	for ; patIdx < patLen; patIdx++ {
		if strings.IndexAny(components[patIdx], "*?[{\\") >= 0 {
			break
		}
	}
	if patIdx > 0 {
		basedir = filepath.Join(basedir, filepath.Join(components[0:patIdx]...))
	}

	// exit early if path is ignored
	if isIgnored(basedir, ignores) {
		return nil
	}

	// Stat will return an error if the file/directory doesn't exist
	fi, err := os.Lstat(basedir)
	if err != nil {
		return nil
	}

	// if there are no more components, we've found a match
	if patIdx >= patLen {
		if traverseDirs && fi.IsDir() {
			// if traverse is enabled, and it is a dir, traverse it
			err := traverseLeaf(matches, basedir, fi, ignores)
			if err != nil {
				return err
			}
		} else {
			// otherwise add it, no need to check for ignores here as it is
			// done above
			matches[basedir] = fi
		}
		return nil
	}

	// otherwise, we need to check each item in the directory...
	// first, if basedir is a symlink, follow it...
	if fi.Mode()&os.ModeSymlink != 0 {
		fi, err = os.Stat(basedir)
		if err != nil {
			return nil
		}
	}

	// confirm it's a directory...
	if !fi.IsDir() {
		return nil
	}

	// read directory
	files, err := readDir(basedir)
	if err != nil {
		return nil
	}
	lastComponent := patIdx+1 >= patLen
	if components[patIdx] == "**" {
		// if the current component is a doublestar, we'll try depth-first
		for _, file := range files {
			// if symlink, we may want to follow
			if file.Mode()&os.ModeSymlink != 0 {
				file, err = os.Stat(filepath.Join(basedir, file.Name()))
				if err != nil {
					continue
				}
			}

			path := filepath.Join(basedir, file.Name())
			if file.IsDir() {
				err := doGlob(matches, path, components[patIdx:], ignores, traverseDirs)
				if err != nil {
					return err
				}
			} else if lastComponent {
				// if the pattern's last component is a doublestar, we match
				// filenames, too
				if !isIgnored(path, ignores) {
					// add it
					matches[path] = file
				}
			}
		}
		if lastComponent {
			return nil
		}
		patIdx++
		lastComponent = patIdx+1 >= patLen
	}

	for _, file := range files {
		match, err := matchComponent(components[patIdx], file.Name())
		if err != nil {
			return err
		}
		if match {
			path := filepath.Join(basedir, file.Name())
			if lastComponent {
				if file.IsDir() && traverseDirs {
					// if traverse is enabled, and it is a dir, traverse it
					err := traverseLeaf(matches, path, file, ignores)
					if err != nil {
						return err
					}
				} else {
					if !isIgnored(path, ignores) {
						// otherwise add it
						matches[path] = file
					}
				}
			} else {
				err := doGlob(matches, path, components[patIdx+1:], ignores, traverseDirs)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func traverseLeaf(matches map[string]os.FileInfo, path string, file os.FileInfo, ignores []string) error {
	// check if ignored
	if isIgnored(path, ignores) {
		return nil
	}
	// if it's not a directory, add to matches and exit
	if !file.IsDir() {
		// add to map
		matches[path] = file
		return nil
	}
	// read directory contents
	files, err := readDir(path)
	if err != nil {
		return err
	}
	// for each child
	for _, fi := range files {
		// create subpath
		subpath := filepath.Join(path, fi.Name())
		err := traverseLeaf(matches, subpath, fi, ignores)
		if err != nil {
			return err
		}
	}
	return nil
}

func isSameOrSubDir(child, parent string) bool {
	rel, err := filepath.Rel(child, parent)
	if err != nil {
		return false
	}
	return strings.HasSuffix(rel, ".") || strings.HasSuffix(rel, "..")
}

func isIgnored(path string, ignores []string) bool {
	if ignores == nil || len(ignores) == 0 {
		return false
	}
	for _, ignore := range ignores {
		if isSameOrSubDir(path, ignore) {
			return true
		}
	}
	return false
}

func readDir(path string) ([]os.FileInfo, error) {
	// similar to ioutil.ReadDir execept it doesn't sort by lexicographical
	// order
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	list, err := f.Readdir(-1)
	f.Close()
	if err != nil {
		return nil, err
	}
	return list, nil
}

func splitPathOnSeparator(path string, separator rune) []string {
	// if the separator is '\\', then we can just split...
	if separator == '\\' {
		return strings.Split(path, string(separator))
	}

	// otherwise, we need to be careful of situations where the separator was escaped
	cnt := strings.Count(path, string(separator))
	if cnt == 0 {
		return []string{path}
	}
	ret := make([]string, cnt+1)
	pathlen := len(path)
	separatorLen := utf8.RuneLen(separator)
	idx := 0
	for start := 0; start < pathlen; {
		end := indexRuneWithEscaping(path[start:], separator)
		if end == -1 {
			end = pathlen
		} else {
			end += start
		}
		ret[idx] = path[start:end]
		start = end + separatorLen
		idx++
	}
	return ret[:idx]
}

func indexRuneWithEscaping(s string, r rune) int {
	end := strings.IndexRune(s, r)
	if end == -1 {
		return -1
	}
	if end > 0 && s[end-1] == '\\' {
		start := end + utf8.RuneLen(r)
		end = indexRuneWithEscaping(s[start:], r)
		if end != -1 {
			end += start
		}
	}
	return end
}

func matchComponent(pattern, name string) (bool, error) {
	patternLen, nameLen := len(pattern), len(name)
	if patternLen == 0 && nameLen == 0 {
		return true, nil
	}
	if patternLen == 0 {
		return false, nil
	}
	if nameLen == 0 && pattern != "*" {
		return false, nil
	}
	patIdx, nameIdx := 0, 0
	for patIdx < patternLen && nameIdx < nameLen {
		patRune, patAdj := utf8.DecodeRuneInString(pattern[patIdx:])
		nameRune, nameAdj := utf8.DecodeRuneInString(name[nameIdx:])
		if patRune == '\\' {
			patIdx += patAdj
			patRune, patAdj = utf8.DecodeRuneInString(pattern[patIdx:])
			if patRune == utf8.RuneError {
				return false, ErrBadPattern
			} else if patRune == nameRune {
				patIdx += patAdj
				nameIdx += nameAdj
			} else {
				return false, nil
			}
		} else if patRune == '*' {
			if patIdx += patAdj; patIdx >= patternLen {
				return true, nil
			}
			for ; nameIdx < nameLen; nameIdx += nameAdj {
				if m, _ := matchComponent(pattern[patIdx:], name[nameIdx:]); m {
					return true, nil
				}
			}
			return false, nil
		} else if patRune == '[' {
			patIdx += patAdj
			endClass := indexRuneWithEscaping(pattern[patIdx:], ']')
			if endClass == -1 {
				return false, ErrBadPattern
			}
			endClass += patIdx
			classRunes := []rune(pattern[patIdx:endClass])
			classRunesLen := len(classRunes)
			if classRunesLen > 0 {
				classIdx := 0
				matchClass := false
				if classRunes[0] == '^' {
					classIdx++
				}
				for classIdx < classRunesLen {
					low := classRunes[classIdx]
					if low == '-' {
						return false, ErrBadPattern
					}
					classIdx++
					if low == '\\' {
						if classIdx < classRunesLen {
							low = classRunes[classIdx]
							classIdx++
						} else {
							return false, ErrBadPattern
						}
					}
					high := low
					if classIdx < classRunesLen && classRunes[classIdx] == '-' {
						if classIdx++; classIdx >= classRunesLen {
							return false, ErrBadPattern
						}
						high = classRunes[classIdx]
						if high == '-' {
							return false, ErrBadPattern
						}
						classIdx++
						if high == '\\' {
							if classIdx < classRunesLen {
								high = classRunes[classIdx]
								classIdx++
							} else {
								return false, ErrBadPattern
							}
						}
					}
					if low <= nameRune && nameRune <= high {
						matchClass = true
					}
				}
				if matchClass == (classRunes[0] == '^') {
					return false, nil
				}
			} else {
				return false, ErrBadPattern
			}
			patIdx = endClass + 1
			nameIdx += nameAdj
		} else if patRune == '{' {
			patIdx += patAdj
			endOptions := indexRuneWithEscaping(pattern[patIdx:], '}')
			if endOptions == -1 {
				return false, ErrBadPattern
			}
			endOptions += patIdx
			options := splitPathOnSeparator(pattern[patIdx:endOptions], ',')
			patIdx = endOptions + 1
			for _, o := range options {
				m, e := matchComponent(o+pattern[patIdx:], name[nameIdx:])
				if e != nil {
					return false, e
				}
				if m {
					return true, nil
				}
			}
			return false, nil
		} else if patRune == '?' || patRune == nameRune {
			patIdx += patAdj
			nameIdx += nameAdj
		} else {
			return false, nil
		}
	}
	if patIdx >= patternLen && nameIdx >= nameLen {
		return true, nil
	}
	if nameIdx >= nameLen && pattern[patIdx:] == "*" || pattern[patIdx:] == "**" {
		return true, nil
	}
	return false, nil
}
