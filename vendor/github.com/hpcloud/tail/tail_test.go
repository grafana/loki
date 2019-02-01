// Copyright (c) 2015 HPE Software Inc. All rights reserved.
// Copyright (c) 2013 ActiveState Software Inc. All rights reserved.

// TODO:
//  * repeat all the tests with Poll:true

package tail

import (
	_ "fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hpcloud/tail/ratelimiter"
	"github.com/hpcloud/tail/watch"
)

func init() {
	// Clear the temporary test directory
	err := os.RemoveAll(".test")
	if err != nil {
		panic(err)
	}
}

func TestMain(m *testing.M) {
	// Use a smaller poll duration for faster test runs. Keep it below
	// 100ms (which value is used as common delays for tests)
	watch.POLL_DURATION = 5 * time.Millisecond
	os.Exit(m.Run())
}

func TestMustExist(t *testing.T) {
	tail, err := TailFile("/no/such/file", Config{Follow: true, MustExist: true})
	if err == nil {
		t.Error("MustExist:true is violated")
		tail.Stop()
	}
	tail, err = TailFile("/no/such/file", Config{Follow: true, MustExist: false})
	if err != nil {
		t.Error("MustExist:false is violated")
	}
	tail.Stop()
	_, err = TailFile("README.md", Config{Follow: true, MustExist: true})
	if err != nil {
		t.Error("MustExist:true on an existing file is violated")
	}
	tail.Cleanup()
}

func TestWaitsForFileToExist(t *testing.T) {
	tailTest := NewTailTest("waits-for-file-to-exist", t)
	tail := tailTest.StartTail("test.txt", Config{})
	go tailTest.VerifyTailOutput(tail, []string{"hello", "world"}, false)

	<-time.After(100 * time.Millisecond)
	tailTest.CreateFile("test.txt", "hello\nworld\n")
	tailTest.Cleanup(tail, true)
}

func TestWaitsForFileToExistRelativePath(t *testing.T) {
	tailTest := NewTailTest("waits-for-file-to-exist-relative", t)

	oldWD, err := os.Getwd()
	if err != nil {
		tailTest.Fatal(err)
	}
	os.Chdir(tailTest.path)
	defer os.Chdir(oldWD)

	tail, err := TailFile("test.txt", Config{})
	if err != nil {
		tailTest.Fatal(err)
	}

	go tailTest.VerifyTailOutput(tail, []string{"hello", "world"}, false)

	<-time.After(100 * time.Millisecond)
	if err := ioutil.WriteFile("test.txt", []byte("hello\nworld\n"), 0600); err != nil {
		tailTest.Fatal(err)
	}
	tailTest.Cleanup(tail, true)
}

func TestStop(t *testing.T) {
	tail, err := TailFile("_no_such_file", Config{Follow: true, MustExist: false})
	if err != nil {
		t.Error("MustExist:false is violated")
	}
	if tail.Stop() != nil {
		t.Error("Should be stoped successfully")
	}
	tail.Cleanup()
}

func TestStopAtEOF(t *testing.T) {
	tailTest := NewTailTest("maxlinesize", t)
	tailTest.CreateFile("test.txt", "hello\nthere\nworld\n")
	tail := tailTest.StartTail("test.txt", Config{Follow: true, Location: nil})

	// read "hello"
	line := <-tail.Lines
	if line.Text != "hello" {
		t.Errorf("Expected to get 'hello', got '%s' instead", line.Text)
	}

	tailTest.VerifyTailOutput(tail, []string{"there", "world"}, false)
	tail.StopAtEOF()
	tailTest.Cleanup(tail, true)
}

func TestMaxLineSizeFollow(t *testing.T) {
	// As last file line does not end with newline, it will not be present in tail's output
	maxLineSize(t, true, "hello\nworld\nfin\nhe", []string{"hel", "lo", "wor", "ld", "fin"})
}

func TestMaxLineSizeNoFollow(t *testing.T) {
	maxLineSize(t, false, "hello\nworld\nfin\nhe", []string{"hel", "lo", "wor", "ld", "fin", "he"})
}

func TestOver4096ByteLine(t *testing.T) {
	tailTest := NewTailTest("Over4096ByteLine", t)
	testString := strings.Repeat("a", 4097)
	tailTest.CreateFile("test.txt", "test\n"+testString+"\nhello\nworld\n")
	tail := tailTest.StartTail("test.txt", Config{Follow: true, Location: nil})
	go tailTest.VerifyTailOutput(tail, []string{"test", testString, "hello", "world"}, false)

	// Delete after a reasonable delay, to give tail sufficient time
	// to read all lines.
	<-time.After(100 * time.Millisecond)
	tailTest.RemoveFile("test.txt")
	tailTest.Cleanup(tail, true)
}
func TestOver4096ByteLineWithSetMaxLineSize(t *testing.T) {
	tailTest := NewTailTest("Over4096ByteLineMaxLineSize", t)
	testString := strings.Repeat("a", 4097)
	tailTest.CreateFile("test.txt", "test\n"+testString+"\nhello\nworld\n")
	tail := tailTest.StartTail("test.txt", Config{Follow: true, Location: nil, MaxLineSize: 4097})
	go tailTest.VerifyTailOutput(tail, []string{"test", testString, "hello", "world"}, false)

	// Delete after a reasonable delay, to give tail sufficient time
	// to read all lines.
	<-time.After(100 * time.Millisecond)
	tailTest.RemoveFile("test.txt")
	tailTest.Cleanup(tail, true)
}

func TestLocationFull(t *testing.T) {
	tailTest := NewTailTest("location-full", t)
	tailTest.CreateFile("test.txt", "hello\nworld\n")
	tail := tailTest.StartTail("test.txt", Config{Follow: true, Location: nil})
	go tailTest.VerifyTailOutput(tail, []string{"hello", "world"}, false)

	// Delete after a reasonable delay, to give tail sufficient time
	// to read all lines.
	<-time.After(100 * time.Millisecond)
	tailTest.RemoveFile("test.txt")
	tailTest.Cleanup(tail, true)
}

func TestLocationFullDontFollow(t *testing.T) {
	tailTest := NewTailTest("location-full-dontfollow", t)
	tailTest.CreateFile("test.txt", "hello\nworld\n")
	tail := tailTest.StartTail("test.txt", Config{Follow: false, Location: nil})
	go tailTest.VerifyTailOutput(tail, []string{"hello", "world"}, false)

	// Add more data only after reasonable delay.
	<-time.After(100 * time.Millisecond)
	tailTest.AppendFile("test.txt", "more\ndata\n")
	<-time.After(100 * time.Millisecond)

	tailTest.Cleanup(tail, true)
}

func TestLocationEnd(t *testing.T) {
	tailTest := NewTailTest("location-end", t)
	tailTest.CreateFile("test.txt", "hello\nworld\n")
	tail := tailTest.StartTail("test.txt", Config{Follow: true, Location: &SeekInfo{0, os.SEEK_END}})
	go tailTest.VerifyTailOutput(tail, []string{"more", "data"}, false)

	<-time.After(100 * time.Millisecond)
	tailTest.AppendFile("test.txt", "more\ndata\n")

	// Delete after a reasonable delay, to give tail sufficient time
	// to read all lines.
	<-time.After(100 * time.Millisecond)
	tailTest.RemoveFile("test.txt")
	tailTest.Cleanup(tail, true)
}

func TestLocationMiddle(t *testing.T) {
	// Test reading from middle.
	tailTest := NewTailTest("location-middle", t)
	tailTest.CreateFile("test.txt", "hello\nworld\n")
	tail := tailTest.StartTail("test.txt", Config{Follow: true, Location: &SeekInfo{-6, os.SEEK_END}})
	go tailTest.VerifyTailOutput(tail, []string{"world", "more", "data"}, false)

	<-time.After(100 * time.Millisecond)
	tailTest.AppendFile("test.txt", "more\ndata\n")

	// Delete after a reasonable delay, to give tail sufficient time
	// to read all lines.
	<-time.After(100 * time.Millisecond)
	tailTest.RemoveFile("test.txt")
	tailTest.Cleanup(tail, true)
}

// The use of polling file watcher could affect file rotation
// (detected via renames), so test these explicitly.

func TestReOpenInotify(t *testing.T) {
	reOpen(t, false)
}

func TestReOpenPolling(t *testing.T) {
	reOpen(t, true)
}

// The use of polling file watcher could affect file rotation
// (detected via renames), so test these explicitly.

func TestReSeekInotify(t *testing.T) {
	reSeek(t, false)
}

func TestReSeekPolling(t *testing.T) {
	reSeek(t, true)
}

func TestRateLimiting(t *testing.T) {
	tailTest := NewTailTest("rate-limiting", t)
	tailTest.CreateFile("test.txt", "hello\nworld\nagain\nextra\n")
	config := Config{
		Follow:      true,
		RateLimiter: ratelimiter.NewLeakyBucket(2, time.Second)}
	leakybucketFull := "Too much log activity; waiting a second before resuming tailing"
	tail := tailTest.StartTail("test.txt", config)

	// TODO: also verify that tail resumes after the cooloff period.
	go tailTest.VerifyTailOutput(tail, []string{
		"hello", "world", "again",
		leakybucketFull,
		"more", "data",
		leakybucketFull}, false)

	// Add more data only after reasonable delay.
	<-time.After(1200 * time.Millisecond)
	tailTest.AppendFile("test.txt", "more\ndata\n")

	// Delete after a reasonable delay, to give tail sufficient time
	// to read all lines.
	<-time.After(100 * time.Millisecond)
	tailTest.RemoveFile("test.txt")

	tailTest.Cleanup(tail, true)
}

func TestTell(t *testing.T) {
	tailTest := NewTailTest("tell-position", t)
	tailTest.CreateFile("test.txt", "hello\nworld\nagain\nmore\n")
	config := Config{
		Follow:   false,
		Location: &SeekInfo{0, os.SEEK_SET}}
	tail := tailTest.StartTail("test.txt", config)
	// read noe line
	<-tail.Lines
	offset, err := tail.Tell()
	if err != nil {
		tailTest.Errorf("Tell return error: %s", err.Error())
	}
	tail.Done()
	// tail.close()

	config = Config{
		Follow:   false,
		Location: &SeekInfo{offset, os.SEEK_SET}}
	tail = tailTest.StartTail("test.txt", config)
	for l := range tail.Lines {
		// it may readed one line in the chan(tail.Lines),
		// so it may lost one line.
		if l.Text != "world" && l.Text != "again" {
			tailTest.Fatalf("mismatch; expected world or again, but got %s",
				l.Text)
		}
		break
	}
	tailTest.RemoveFile("test.txt")
	tail.Done()
	tail.Cleanup()
}

func TestBlockUntilExists(t *testing.T) {
	tailTest := NewTailTest("block-until-file-exists", t)
	config := Config{
		Follow: true,
	}
	tail := tailTest.StartTail("test.txt", config)
	go func() {
		time.Sleep(100 * time.Millisecond)
		tailTest.CreateFile("test.txt", "hello world\n")
	}()
	for l := range tail.Lines {
		if l.Text != "hello world" {
			tailTest.Fatalf("mismatch; expected hello world, but got %s",
				l.Text)
		}
		break
	}
	tailTest.RemoveFile("test.txt")
	tail.Stop()
	tail.Cleanup()
}

func maxLineSize(t *testing.T, follow bool, fileContent string, expected []string) {
	tailTest := NewTailTest("maxlinesize", t)
	tailTest.CreateFile("test.txt", fileContent)
	tail := tailTest.StartTail("test.txt", Config{Follow: follow, Location: nil, MaxLineSize: 3})
	go tailTest.VerifyTailOutput(tail, expected, false)

	// Delete after a reasonable delay, to give tail sufficient time
	// to read all lines.
	<-time.After(100 * time.Millisecond)
	tailTest.RemoveFile("test.txt")
	tailTest.Cleanup(tail, true)
}

func reOpen(t *testing.T, poll bool) {
	var name string
	var delay time.Duration
	if poll {
		name = "reopen-polling"
		delay = 300 * time.Millisecond // account for POLL_DURATION
	} else {
		name = "reopen-inotify"
		delay = 100 * time.Millisecond
	}
	tailTest := NewTailTest(name, t)
	tailTest.CreateFile("test.txt", "hello\nworld\n")
	tail := tailTest.StartTail(
		"test.txt",
		Config{Follow: true, ReOpen: true, Poll: poll})
	content := []string{"hello", "world", "more", "data", "endofworld"}
	go tailTest.ReadLines(tail, content)

	// deletion must trigger reopen
	<-time.After(delay)
	tailTest.RemoveFile("test.txt")
	<-time.After(delay)
	tailTest.CreateFile("test.txt", "more\ndata\n")

	// rename must trigger reopen
	<-time.After(delay)
	tailTest.RenameFile("test.txt", "test.txt.rotated")
	<-time.After(delay)
	tailTest.CreateFile("test.txt", "endofworld\n")

	// Delete after a reasonable delay, to give tail sufficient time
	// to read all lines.
	<-time.After(delay)
	tailTest.RemoveFile("test.txt")
	<-time.After(delay)

	// Do not bother with stopping as it could kill the tomb during
	// the reading of data written above. Timings can vary based on
	// test environment.
	tail.Cleanup()
}

func reSeek(t *testing.T, poll bool) {
	var name string
	if poll {
		name = "reseek-polling"
	} else {
		name = "reseek-inotify"
	}
	tailTest := NewTailTest(name, t)
	tailTest.CreateFile("test.txt", "a really long string goes here\nhello\nworld\n")
	tail := tailTest.StartTail(
		"test.txt",
		Config{Follow: true, ReOpen: false, Poll: poll})

	go tailTest.VerifyTailOutput(tail, []string{
		"a really long string goes here", "hello", "world", "h311o", "w0r1d", "endofworld"}, false)

	// truncate now
	<-time.After(100 * time.Millisecond)
	tailTest.TruncateFile("test.txt", "h311o\nw0r1d\nendofworld\n")

	// Delete after a reasonable delay, to give tail sufficient time
	// to read all lines.
	<-time.After(100 * time.Millisecond)
	tailTest.RemoveFile("test.txt")

	// Do not bother with stopping as it could kill the tomb during
	// the reading of data written above. Timings can vary based on
	// test environment.
	tailTest.Cleanup(tail, false)
}

// Test library

type TailTest struct {
	Name string
	path string
	done chan struct{}
	*testing.T
}

func NewTailTest(name string, t *testing.T) TailTest {
	tt := TailTest{name, ".test/" + name, make(chan struct{}), t}
	err := os.MkdirAll(tt.path, os.ModeTemporary|0700)
	if err != nil {
		tt.Fatal(err)
	}

	return tt
}

func (t TailTest) CreateFile(name string, contents string) {
	err := ioutil.WriteFile(t.path+"/"+name, []byte(contents), 0600)
	if err != nil {
		t.Fatal(err)
	}
}

func (t TailTest) RemoveFile(name string) {
	err := os.Remove(t.path + "/" + name)
	if err != nil {
		t.Fatal(err)
	}
}

func (t TailTest) RenameFile(oldname string, newname string) {
	oldname = t.path + "/" + oldname
	newname = t.path + "/" + newname
	err := os.Rename(oldname, newname)
	if err != nil {
		t.Fatal(err)
	}
}

func (t TailTest) AppendFile(name string, contents string) {
	f, err := os.OpenFile(t.path+"/"+name, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, err = f.WriteString(contents)
	if err != nil {
		t.Fatal(err)
	}
}

func (t TailTest) TruncateFile(name string, contents string) {
	f, err := os.OpenFile(t.path+"/"+name, os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, err = f.WriteString(contents)
	if err != nil {
		t.Fatal(err)
	}
}

func (t TailTest) StartTail(name string, config Config) *Tail {
	tail, err := TailFile(t.path+"/"+name, config)
	if err != nil {
		t.Fatal(err)
	}
	return tail
}

func (t TailTest) VerifyTailOutput(tail *Tail, lines []string, expectEOF bool) {
	defer close(t.done)
	t.ReadLines(tail, lines)
	// It is important to do this if only EOF is expected
	// otherwise we could block on <-tail.Lines
	if expectEOF {
		line, ok := <-tail.Lines
		if ok {
			t.Fatalf("more content from tail: %+v", line)
		}
	}
}

func (t TailTest) ReadLines(tail *Tail, lines []string) {
	for idx, line := range lines {
		tailedLine, ok := <-tail.Lines
		if !ok {
			// tail.Lines is closed and empty.
			err := tail.Err()
			if err != nil {
				t.Fatalf("tail ended with error: %v", err)
			}
			t.Fatalf("tail ended early; expecting more: %v", lines[idx:])
		}
		if tailedLine == nil {
			t.Fatalf("tail.Lines returned nil; not possible")
		}
		// Note: not checking .Err as the `lines` argument is designed
		// to match error strings as well.
		if tailedLine.Text != line {
			t.Fatalf(
				"unexpected line/err from tail: "+
					"expecting <<%s>>>, but got <<<%s>>>",
				line, tailedLine.Text)
		}
	}
}

func (t TailTest) Cleanup(tail *Tail, stop bool) {
	<-t.done
	if stop {
		tail.Stop()
	}
	tail.Cleanup()
}
