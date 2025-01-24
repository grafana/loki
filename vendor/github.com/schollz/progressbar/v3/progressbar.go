package progressbar

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mitchellh/colorstring"
	"github.com/rivo/uniseg"
	"golang.org/x/term"
)

// ProgressBar is a thread-safe, simple
// progress bar
type ProgressBar struct {
	state  state
	config config
	lock   sync.Mutex
}

// State is the basic properties of the bar
type State struct {
	Max            int64
	CurrentNum     int64
	CurrentPercent float64
	CurrentBytes   float64
	SecondsSince   float64
	SecondsLeft    float64
	KBsPerSecond   float64
	Description    string
}

type state struct {
	currentNum        int64
	currentPercent    int
	lastPercent       int
	currentSaucerSize int
	isAltSaucerHead   bool

	lastShown time.Time
	startTime time.Time // time when the progress bar start working

	counterTime         time.Time
	counterNumSinceLast int64
	counterLastTenRates []float64
	spinnerIdx          int // the index of spinner

	maxLineWidth int
	currentBytes float64
	finished     bool
	exit         bool // Progress bar exit halfway

	details []string // details to show,only used when detail row is set to more than 0

	rendered string
}

type config struct {
	max                  int64 // max number of the counter
	maxHumanized         string
	maxHumanizedSuffix   string
	width                int
	writer               io.Writer
	theme                Theme
	renderWithBlankState bool
	description          string
	iterationString      string
	ignoreLength         bool // ignoreLength if max bytes not known

	// whether the output is expected to contain color codes
	colorCodes bool

	// show rate of change in kB/sec or MB/sec
	showBytes bool
	// show the iterations per second
	showIterationsPerSecond bool
	showIterationsCount     bool

	// whether the progress bar should show the total bytes (e.g. 23/24 or 23/-, vs. just 23).
	showTotalBytes bool

	// whether the progress bar should show elapsed time.
	// always enabled if predictTime is true.
	elapsedTime bool

	showElapsedTimeOnFinish bool

	// whether the progress bar should attempt to predict the finishing
	// time of the progress based on the start time and the average
	// number of seconds between  increments.
	predictTime bool

	// minimum time to wait in between updates
	throttleDuration time.Duration

	// clear bar once finished
	clearOnFinish bool

	// spinnerType should be a number between 0-75
	spinnerType int

	// spinnerTypeOptionUsed remembers if the spinnerType was changed manually
	spinnerTypeOptionUsed bool

	// spinnerChangeInterval the change interval of spinner
	// if set this attribute to 0, the spinner only change when renderProgressBar was called
	// for example, each time when Add() was called,which will call renderProgressBar function
	spinnerChangeInterval time.Duration

	// spinner represents the spinner as a slice of string
	spinner []string

	// fullWidth specifies whether to measure and set the bar to a specific width
	fullWidth bool

	// invisible doesn't render the bar at all, useful for debugging
	invisible bool

	onCompletion func()

	// whether the render function should make use of ANSI codes to reduce console I/O
	useANSICodes bool

	// whether to use the IEC units (e.g. MiB) instead of the default SI units (e.g. MB)
	useIECUnits bool

	// showDescriptionAtLineEnd specifies whether description should be written at line end instead of line start
	showDescriptionAtLineEnd bool

	// specifies how many rows of details to show,default value is 0 and no details will be shown
	maxDetailRow int

	stdBuffer bytes.Buffer
}

// Theme defines the elements of the bar
type Theme struct {
	Saucer        string
	AltSaucerHead string
	SaucerHead    string
	SaucerPadding string
	BarStart      string
	BarEnd        string

	// BarStartFilled is used after the Bar starts filling, if set. Otherwise, it defaults to BarStart.
	BarStartFilled string

	// BarEndFilled is used once the Bar finishes, if set. Otherwise, it defaults to BarEnd.
	BarEndFilled string
}

var (
	// ThemeDefault is given by default (if not changed with OptionSetTheme), and it looks like "|████     |".
	ThemeDefault = Theme{Saucer: "█", SaucerPadding: " ", BarStart: "|", BarEnd: "|"}

	// ThemeASCII is a predefined Theme that uses ASCII symbols. It looks like "[===>...]".
	// Configure it with OptionSetTheme(ThemeASCII).
	ThemeASCII = Theme{
		Saucer:        "=",
		SaucerHead:    ">",
		SaucerPadding: ".",
		BarStart:      "[",
		BarEnd:        "]",
	}

	// ThemeUnicode is a predefined Theme that uses Unicode characters, displaying a graphic bar.
	// It looks like "" (rendering will depend on font being used).
	// It requires special symbols usually found in "nerd fonts" [2], or in Fira Code [1], and other sources.
	// Configure it with OptionSetTheme(ThemeUnicode).
	//
	// [1] https://github.com/tonsky/FiraCode
	// [2] https://www.nerdfonts.com/
	ThemeUnicode = Theme{
		Saucer:         "\uEE04", // 
		SaucerHead:     "\uEE04", // 
		SaucerPadding:  "\uEE01", // 
		BarStart:       "\uEE00", // 
		BarStartFilled: "\uEE03", // 
		BarEnd:         "\uEE02", // 
		BarEndFilled:   "\uEE05", // 
	}
)

// Option is the type all options need to adhere to
type Option func(p *ProgressBar)

// OptionSetWidth sets the width of the bar
func OptionSetWidth(s int) Option {
	return func(p *ProgressBar) {
		p.config.width = s
	}
}

// OptionSetSpinnerChangeInterval sets the spinner change interval
// the spinner will change according to this value.
// By default, this value is 100 * time.Millisecond
// If you don't want to let this progressbar update by specified time interval
// you can  set this value to zero, then the spinner will change each time rendered,
// such as when Add() or Describe() was called
func OptionSetSpinnerChangeInterval(interval time.Duration) Option {
	return func(p *ProgressBar) {
		p.config.spinnerChangeInterval = interval
	}
}

// OptionSpinnerType sets the type of spinner used for indeterminate bars
func OptionSpinnerType(spinnerType int) Option {
	return func(p *ProgressBar) {
		p.config.spinnerTypeOptionUsed = true
		p.config.spinnerType = spinnerType
	}
}

// OptionSpinnerCustom sets the spinner used for indeterminate bars to the passed
// slice of string
func OptionSpinnerCustom(spinner []string) Option {
	return func(p *ProgressBar) {
		p.config.spinner = spinner
	}
}

// OptionSetTheme sets the elements the bar is constructed with.
// There are two pre-defined themes you can use: ThemeASCII and ThemeUnicode.
func OptionSetTheme(t Theme) Option {
	return func(p *ProgressBar) {
		p.config.theme = t
	}
}

// OptionSetVisibility sets the visibility
func OptionSetVisibility(visibility bool) Option {
	return func(p *ProgressBar) {
		p.config.invisible = !visibility
	}
}

// OptionFullWidth sets the bar to be full width
func OptionFullWidth() Option {
	return func(p *ProgressBar) {
		p.config.fullWidth = true
	}
}

// OptionSetWriter sets the output writer (defaults to os.StdOut)
func OptionSetWriter(w io.Writer) Option {
	return func(p *ProgressBar) {
		p.config.writer = w
	}
}

// OptionSetRenderBlankState sets whether or not to render a 0% bar on construction
func OptionSetRenderBlankState(r bool) Option {
	return func(p *ProgressBar) {
		p.config.renderWithBlankState = r
	}
}

// OptionSetDescription sets the description of the bar to render in front of it
func OptionSetDescription(description string) Option {
	return func(p *ProgressBar) {
		p.config.description = description
	}
}

// OptionEnableColorCodes enables or disables support for color codes
// using mitchellh/colorstring
func OptionEnableColorCodes(colorCodes bool) Option {
	return func(p *ProgressBar) {
		p.config.colorCodes = colorCodes
	}
}

// OptionSetElapsedTime will enable elapsed time. Always enabled if OptionSetPredictTime is true.
func OptionSetElapsedTime(elapsedTime bool) Option {
	return func(p *ProgressBar) {
		p.config.elapsedTime = elapsedTime
	}
}

// OptionSetPredictTime will also attempt to predict the time remaining.
func OptionSetPredictTime(predictTime bool) Option {
	return func(p *ProgressBar) {
		p.config.predictTime = predictTime
	}
}

// OptionShowCount will also print current count out of total
func OptionShowCount() Option {
	return func(p *ProgressBar) {
		p.config.showIterationsCount = true
	}
}

// OptionShowIts will also print the iterations/second
func OptionShowIts() Option {
	return func(p *ProgressBar) {
		p.config.showIterationsPerSecond = true
	}
}

// OptionShowElapsedTimeOnFinish will keep the display of elapsed time on finish.
func OptionShowElapsedTimeOnFinish() Option {
	return func(p *ProgressBar) {
		p.config.showElapsedTimeOnFinish = true
	}
}

// OptionShowTotalBytes will keep the display of total bytes.
func OptionShowTotalBytes(flag bool) Option {
	return func(p *ProgressBar) {
		p.config.showTotalBytes = flag
	}
}

// OptionSetItsString sets what's displayed for iterations a second. The default is "it" which would display: "it/s"
func OptionSetItsString(iterationString string) Option {
	return func(p *ProgressBar) {
		p.config.iterationString = iterationString
	}
}

// OptionThrottle will wait the specified duration before updating again. The default
// duration is 0 seconds.
func OptionThrottle(duration time.Duration) Option {
	return func(p *ProgressBar) {
		p.config.throttleDuration = duration
	}
}

// OptionClearOnFinish will clear the bar once its finished.
func OptionClearOnFinish() Option {
	return func(p *ProgressBar) {
		p.config.clearOnFinish = true
	}
}

// OptionOnCompletion will invoke cmpl function once its finished
func OptionOnCompletion(cmpl func()) Option {
	return func(p *ProgressBar) {
		p.config.onCompletion = cmpl
	}
}

// OptionShowBytes will update the progress bar
// configuration settings to display/hide kBytes/Sec
func OptionShowBytes(val bool) Option {
	return func(p *ProgressBar) {
		p.config.showBytes = val
	}
}

// OptionUseANSICodes will use more optimized terminal i/o.
//
// Only useful in environments with support for ANSI escape sequences.
func OptionUseANSICodes(val bool) Option {
	return func(p *ProgressBar) {
		p.config.useANSICodes = val
	}
}

// OptionUseIECUnits will enable IEC units (e.g. MiB) instead of the default
// SI units (e.g. MB).
func OptionUseIECUnits(val bool) Option {
	return func(p *ProgressBar) {
		p.config.useIECUnits = val
	}
}

// OptionShowDescriptionAtLineEnd defines whether description should be written at line end instead of line start
func OptionShowDescriptionAtLineEnd() Option {
	return func(p *ProgressBar) {
		p.config.showDescriptionAtLineEnd = true
	}
}

// OptionSetMaxDetailRow sets the max row of details
// the row count should be less than the terminal height, otherwise it will not give you the output you want
func OptionSetMaxDetailRow(row int) Option {
	return func(p *ProgressBar) {
		p.config.maxDetailRow = row
	}
}

// NewOptions constructs a new instance of ProgressBar, with any options you specify
func NewOptions(max int, options ...Option) *ProgressBar {
	return NewOptions64(int64(max), options...)
}

// NewOptions64 constructs a new instance of ProgressBar, with any options you specify
func NewOptions64(max int64, options ...Option) *ProgressBar {
	b := ProgressBar{
		state: state{
			startTime:   time.Time{},
			lastShown:   time.Time{},
			counterTime: time.Time{},
		},
		config: config{
			writer:                os.Stdout,
			theme:                 ThemeDefault,
			iterationString:       "it",
			width:                 40,
			max:                   max,
			throttleDuration:      0 * time.Nanosecond,
			elapsedTime:           max == -1,
			predictTime:           true,
			spinnerType:           9,
			invisible:             false,
			spinnerChangeInterval: 100 * time.Millisecond,
			showTotalBytes:        true,
		},
	}

	for _, o := range options {
		o(&b)
	}

	if b.config.spinnerType < 0 || b.config.spinnerType > 75 {
		panic("invalid spinner type, must be between 0 and 75")
	}

	if b.config.maxDetailRow < 0 {
		panic("invalid max detail row, must be greater than 0")
	}

	// ignoreLength if max bytes not known
	if b.config.max == -1 {
		b.lengthUnknown()
	}

	b.config.maxHumanized, b.config.maxHumanizedSuffix = humanizeBytes(float64(b.config.max),
		b.config.useIECUnits)

	if b.config.renderWithBlankState {
		b.RenderBlank()
	}

	// if the render time interval attribute is set
	if b.config.spinnerChangeInterval != 0 && !b.config.invisible && b.config.ignoreLength {
		go func() {
			ticker := time.NewTicker(b.config.spinnerChangeInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if b.IsFinished() {
						return
					}
					if b.IsStarted() {
						b.lock.Lock()
						b.render()
						b.lock.Unlock()
					}
				}
			}
		}()
	}

	return &b
}

func getBasicState() state {
	now := time.Now()
	return state{
		startTime:   now,
		lastShown:   now,
		counterTime: now,
	}
}

// New returns a new ProgressBar
// with the specified maximum
func New(max int) *ProgressBar {
	return NewOptions(max)
}

// DefaultBytes provides a progressbar to measure byte
// throughput with recommended defaults.
// Set maxBytes to -1 to use as a spinner.
func DefaultBytes(maxBytes int64, description ...string) *ProgressBar {
	desc := ""
	if len(description) > 0 {
		desc = description[0]
	}
	return NewOptions64(
		maxBytes,
		OptionSetDescription(desc),
		OptionSetWriter(os.Stderr),
		OptionShowBytes(true),
		OptionShowTotalBytes(true),
		OptionSetWidth(10),
		OptionThrottle(65*time.Millisecond),
		OptionShowCount(),
		OptionOnCompletion(func() {
			fmt.Fprint(os.Stderr, "\n")
		}),
		OptionSpinnerType(14),
		OptionFullWidth(),
		OptionSetRenderBlankState(true),
	)
}

// DefaultBytesSilent is the same as DefaultBytes, but does not output anywhere.
// String() can be used to get the output instead.
func DefaultBytesSilent(maxBytes int64, description ...string) *ProgressBar {
	// Mostly the same bar as DefaultBytes

	desc := ""
	if len(description) > 0 {
		desc = description[0]
	}
	return NewOptions64(
		maxBytes,
		OptionSetDescription(desc),
		OptionSetWriter(io.Discard),
		OptionShowBytes(true),
		OptionShowTotalBytes(true),
		OptionSetWidth(10),
		OptionThrottle(65*time.Millisecond),
		OptionShowCount(),
		OptionSpinnerType(14),
		OptionFullWidth(),
	)
}

// Default provides a progressbar with recommended defaults.
// Set max to -1 to use as a spinner.
func Default(max int64, description ...string) *ProgressBar {
	desc := ""
	if len(description) > 0 {
		desc = description[0]
	}
	return NewOptions64(
		max,
		OptionSetDescription(desc),
		OptionSetWriter(os.Stderr),
		OptionSetWidth(10),
		OptionShowTotalBytes(true),
		OptionThrottle(65*time.Millisecond),
		OptionShowCount(),
		OptionShowIts(),
		OptionOnCompletion(func() {
			fmt.Fprint(os.Stderr, "\n")
		}),
		OptionSpinnerType(14),
		OptionFullWidth(),
		OptionSetRenderBlankState(true),
	)
}

// DefaultSilent is the same as Default, but does not output anywhere.
// String() can be used to get the output instead.
func DefaultSilent(max int64, description ...string) *ProgressBar {
	// Mostly the same bar as Default

	desc := ""
	if len(description) > 0 {
		desc = description[0]
	}
	return NewOptions64(
		max,
		OptionSetDescription(desc),
		OptionSetWriter(io.Discard),
		OptionSetWidth(10),
		OptionShowTotalBytes(true),
		OptionThrottle(65*time.Millisecond),
		OptionShowCount(),
		OptionShowIts(),
		OptionSpinnerType(14),
		OptionFullWidth(),
	)
}

// String returns the current rendered version of the progress bar.
// It will never return an empty string while the progress bar is running.
func (p *ProgressBar) String() string {
	return p.state.rendered
}

// RenderBlank renders the current bar state, you can use this to render a 0% state
func (p *ProgressBar) RenderBlank() error {
	p.lock.Lock()
	defer p.lock.Unlock()

	if p.config.invisible {
		return nil
	}
	if p.state.currentNum == 0 {
		p.state.lastShown = time.Time{}
	}
	return p.render()
}

// StartWithoutRender will start the progress bar without rendering it
// this method is created for the use case where you want to start the progress
// but don't want to render it immediately.
// If you want to start the progress and render it immediately, use RenderBlank instead,
// or maybe you can use Add to start it automatically, but it will make the time calculation less precise.
func (p *ProgressBar) StartWithoutRender() {
	p.lock.Lock()
	defer p.lock.Unlock()

	if p.IsStarted() {
		return
	}

	p.state.startTime = time.Now()
	// the counterTime should be set to the current time
	p.state.counterTime = time.Now()
}

// Reset will reset the clock that is used
// to calculate current time and the time left.
func (p *ProgressBar) Reset() {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.state = getBasicState()
}

// Finish will fill the bar to full
func (p *ProgressBar) Finish() error {
	p.lock.Lock()
	p.state.currentNum = p.config.max
	if !p.config.ignoreLength {
		p.state.currentBytes = float64(p.config.max)
	}
	p.lock.Unlock()
	return p.Add(0)
}

// Exit will exit the bar to keep current state
func (p *ProgressBar) Exit() error {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.state.exit = true
	if p.config.onCompletion != nil {
		p.config.onCompletion()
	}
	return nil
}

// Add will add the specified amount to the progressbar
func (p *ProgressBar) Add(num int) error {
	return p.Add64(int64(num))
}

// Set will set the bar to a current number
func (p *ProgressBar) Set(num int) error {
	return p.Set64(int64(num))
}

// Set64 will set the bar to a current number
func (p *ProgressBar) Set64(num int64) error {
	p.lock.Lock()
	toAdd := num - int64(p.state.currentBytes)
	p.lock.Unlock()
	return p.Add64(toAdd)
}

// Add64 will add the specified amount to the progressbar
func (p *ProgressBar) Add64(num int64) error {
	if p.config.invisible {
		return nil
	}
	p.lock.Lock()
	defer p.lock.Unlock()

	if p.state.exit {
		return nil
	}

	// error out since OptionSpinnerCustom will always override a manually set spinnerType
	if p.config.spinnerTypeOptionUsed && len(p.config.spinner) > 0 {
		return errors.New("OptionSpinnerType and OptionSpinnerCustom cannot be used together")
	}

	if p.config.max == 0 {
		return errors.New("max must be greater than 0")
	}

	if p.state.currentNum < p.config.max {
		if p.config.ignoreLength {
			p.state.currentNum = (p.state.currentNum + num) % p.config.max
		} else {
			p.state.currentNum += num
		}
	}

	p.state.currentBytes += float64(num)

	if p.state.counterTime.IsZero() {
		p.state.counterTime = time.Now()
	}

	// reset the countdown timer every second to take rolling average
	p.state.counterNumSinceLast += num
	if time.Since(p.state.counterTime).Seconds() > 0.5 {
		p.state.counterLastTenRates = append(p.state.counterLastTenRates, float64(p.state.counterNumSinceLast)/time.Since(p.state.counterTime).Seconds())
		if len(p.state.counterLastTenRates) > 10 {
			p.state.counterLastTenRates = p.state.counterLastTenRates[1:]
		}
		p.state.counterTime = time.Now()
		p.state.counterNumSinceLast = 0
	}

	percent := float64(p.state.currentNum) / float64(p.config.max)
	p.state.currentSaucerSize = int(percent * float64(p.config.width))
	p.state.currentPercent = int(percent * 100)
	updateBar := p.state.currentPercent != p.state.lastPercent && p.state.currentPercent > 0

	p.state.lastPercent = p.state.currentPercent
	if p.state.currentNum > p.config.max {
		return errors.New("current number exceeds max")
	}

	// always update if show bytes/second or its/second
	if updateBar || p.config.showIterationsPerSecond || p.config.showIterationsCount {
		return p.render()
	}

	return nil
}

// AddDetail adds a detail to the progress bar. Only used when maxDetailRow is set to a value greater than 0
func (p *ProgressBar) AddDetail(detail string) error {
	if p.config.maxDetailRow == 0 {
		return errors.New("maxDetailRow is set to 0, cannot add detail")
	}
	if p.IsFinished() {
		return errors.New("cannot add detail to a finished progress bar")
	}

	p.lock.Lock()
	defer p.lock.Unlock()
	if p.state.details == nil {
		// if we add a detail before the first add, it will be weird that we have detail but don't have the progress bar in the top.
		// so when we add the first detail, we will render the progress bar first.
		if err := p.render(); err != nil {
			return err
		}
	}
	p.state.details = append(p.state.details, detail)
	if len(p.state.details) > p.config.maxDetailRow {
		p.state.details = p.state.details[1:]
	}
	if err := p.renderDetails(); err != nil {
		return err
	}
	return nil
}

// renderDetails renders the details of the progress bar
func (p *ProgressBar) renderDetails() error {
	if p.config.invisible {
		return nil
	}
	if p.state.finished {
		return nil
	}
	if p.config.maxDetailRow == 0 {
		return nil
	}

	b := strings.Builder{}
	b.WriteString("\n")

	// render the details row
	for _, detail := range p.state.details {
		b.WriteString(fmt.Sprintf("\u001B[K\r%s\n", detail))
	}
	// add empty lines to fill the maxDetailRow
	for i := len(p.state.details); i < p.config.maxDetailRow; i++ {
		b.WriteString("\u001B[K\n")
	}

	// move the cursor up to the start of the details row
	b.WriteString(fmt.Sprintf("\u001B[%dF", p.config.maxDetailRow+1))

	writeString(p.config, b.String())

	return nil
}

// Clear erases the progress bar from the current line
func (p *ProgressBar) Clear() error {
	return clearProgressBar(p.config, p.state)
}

// Describe will change the description shown before the progress, which
// can be changed on the fly (as for a slow running process).
func (p *ProgressBar) Describe(description string) {
	p.lock.Lock()
	defer p.lock.Unlock()
	p.config.description = description
	if p.config.invisible {
		return
	}
	p.render()
}

// New64 returns a new ProgressBar
// with the specified maximum
func New64(max int64) *ProgressBar {
	return NewOptions64(max)
}

// GetMax returns the max of a bar
func (p *ProgressBar) GetMax() int {
	p.lock.Lock()
	defer p.lock.Unlock()

	return int(p.config.max)
}

// GetMax64 returns the current max
func (p *ProgressBar) GetMax64() int64 {
	p.lock.Lock()
	defer p.lock.Unlock()

	return p.config.max
}

// ChangeMax takes in a int
// and changes the max value
// of the progress bar
func (p *ProgressBar) ChangeMax(newMax int) {
	p.ChangeMax64(int64(newMax))
}

// ChangeMax64 is basically
// the same as ChangeMax,
// but takes in a int64
// to avoid casting
func (p *ProgressBar) ChangeMax64(newMax int64) {
	p.lock.Lock()

	p.config.max = newMax

	if p.config.showBytes {
		p.config.maxHumanized, p.config.maxHumanizedSuffix = humanizeBytes(float64(p.config.max),
			p.config.useIECUnits)
	}

	if newMax == -1 {
		p.lengthUnknown()
	} else {
		p.lengthKnown(newMax)
	}
	p.lock.Unlock() // so p.Add can lock

	p.Add(0) // re-render
}

// AddMax takes in a int
// and adds it to the max
// value of the progress bar
func (p *ProgressBar) AddMax(added int) {
	p.AddMax64(int64(added))
}

// AddMax64 is basically
// the same as AddMax,
// but takes in a int64
// to avoid casting
func (p *ProgressBar) AddMax64(added int64) {
	p.lock.Lock()

	p.config.max += added

	if p.config.showBytes {
		p.config.maxHumanized, p.config.maxHumanizedSuffix = humanizeBytes(float64(p.config.max),
			p.config.useIECUnits)
	}

	if p.config.max == -1 {
		p.lengthUnknown()
	} else {
		p.lengthKnown(p.config.max)
	}
	p.lock.Unlock() // so p.Add can lock

	p.Add(0) // re-render
}

// IsFinished returns true if progress bar is completed
func (p *ProgressBar) IsFinished() bool {
	p.lock.Lock()
	defer p.lock.Unlock()

	return p.state.finished
}

// IsStarted returns true if progress bar is started
func (p *ProgressBar) IsStarted() bool {
	return !p.state.startTime.IsZero()
}

// render renders the progress bar, updating the maximum
// rendered line width. this function is not thread-safe,
// so it must be called with an acquired lock.
func (p *ProgressBar) render() error {
	// make sure that the rendering is not happening too quickly
	// but always show if the currentNum reaches the max
	if !p.IsStarted() {
		p.state.startTime = time.Now()
	} else if time.Since(p.state.lastShown).Nanoseconds() < p.config.throttleDuration.Nanoseconds() &&
		p.state.currentNum < p.config.max {
		return nil
	}

	if !p.config.useANSICodes {
		// first, clear the existing progress bar, if not yet finished.
		if !p.state.finished {
			err := clearProgressBar(p.config, p.state)
			if err != nil {
				return err
			}
		}
	}

	// check if the progress bar is finished
	if !p.state.finished && p.state.currentNum >= p.config.max {
		p.state.finished = true
		if !p.config.clearOnFinish {
			io.Copy(p.config.writer, &p.config.stdBuffer)
			renderProgressBar(p.config, &p.state)
		}
		if p.config.maxDetailRow > 0 {
			p.renderDetails()
			// put the cursor back to the last line of the details
			writeString(p.config, fmt.Sprintf("\u001B[%dB\r\u001B[%dC", p.config.maxDetailRow, len(p.state.details[len(p.state.details)-1])))
		}
		if p.config.onCompletion != nil {
			p.config.onCompletion()
		}
	}
	if p.state.finished {
		// when using ANSI codes we don't pre-clean the current line
		if p.config.useANSICodes && p.config.clearOnFinish {
			err := clearProgressBar(p.config, p.state)
			if err != nil {
				return err
			}
		}
		return nil
	}

	// then, re-render the current progress bar
	io.Copy(p.config.writer, &p.config.stdBuffer)
	w, err := renderProgressBar(p.config, &p.state)
	if err != nil {
		return err
	}

	if w > p.state.maxLineWidth {
		p.state.maxLineWidth = w
	}

	p.state.lastShown = time.Now()

	return nil
}

// lengthUnknown sets the progress bar to ignore the length
func (p *ProgressBar) lengthUnknown() {
	p.config.ignoreLength = true
	p.config.max = int64(p.config.width)
	p.config.predictTime = false
}

// lengthKnown sets the progress bar to do not ignore the length
func (p *ProgressBar) lengthKnown(max int64) {
	p.config.ignoreLength = false
	p.config.max = max
	p.config.predictTime = true
}

// State returns the current state
func (p *ProgressBar) State() State {
	p.lock.Lock()
	defer p.lock.Unlock()
	s := State{}
	s.CurrentNum = p.state.currentNum
	s.Max = p.config.max
	if p.config.ignoreLength {
		s.Max = -1
	}
	s.CurrentPercent = float64(p.state.currentNum) / float64(p.config.max)
	s.CurrentBytes = p.state.currentBytes
	if p.IsStarted() {
		s.SecondsSince = time.Since(p.state.startTime).Seconds()
	} else {
		s.SecondsSince = 0
	}

	if p.state.currentNum > 0 {
		s.SecondsLeft = s.SecondsSince / float64(p.state.currentNum) * (float64(p.config.max) - float64(p.state.currentNum))
	}
	s.KBsPerSecond = float64(p.state.currentBytes) / 1024.0 / s.SecondsSince
	s.Description = p.config.description
	return s
}

// StartHTTPServer starts an HTTP server dedicated to serving progress bar updates. This allows you to
// display the status in various UI elements, such as an OS status bar with an `xbar` extension.
// It is recommended to run this function in a separate goroutine to avoid blocking the main thread.
//
// hostPort specifies the address and port to bind the server to, for example, "0.0.0.0:19999".
func (p *ProgressBar) StartHTTPServer(hostPort string) {
	// for advanced users, we can return the data as json
	http.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/json")
		// since the state is a simple struct, we can just ignore the error
		bs, _ := json.Marshal(p.State())
		w.Write(bs)
	})
	// for others, we just return the description in a plain text format
	http.HandleFunc("/desc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w,
			"%d/%d, %.2f%%, %s left",
			p.State().CurrentNum, p.State().Max, p.State().CurrentPercent*100,
			(time.Second * time.Duration(p.State().SecondsLeft)).String(),
		)
	})
	log.Fatal(http.ListenAndServe(hostPort, nil))
}

// regex matching ansi escape codes
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func getStringWidth(c config, str string, colorize bool) int {
	if c.colorCodes {
		// convert any color codes in the progress bar into the respective ANSI codes
		str = colorstring.Color(str)
	}

	// the width of the string, if printed to the console
	// does not include the carriage return character
	cleanString := strings.Replace(str, "\r", "", -1)

	if c.colorCodes {
		// the ANSI codes for the colors do not take up space in the console output,
		// so they do not count towards the output string width
		cleanString = ansiRegex.ReplaceAllString(cleanString, "")
	}

	// get the amount of runes in the string instead of the
	// character count of the string, as some runes span multiple characters.
	// see https://stackoverflow.com/a/12668840/2733724
	stringWidth := uniseg.StringWidth(cleanString)
	return stringWidth
}

func renderProgressBar(c config, s *state) (int, error) {
	var sb strings.Builder

	averageRate := average(s.counterLastTenRates)
	if len(s.counterLastTenRates) == 0 || s.finished {
		// if no average samples, or if finished,
		// then average rate should be the total rate
		if t := time.Since(s.startTime).Seconds(); t > 0 {
			averageRate = s.currentBytes / t
		} else {
			averageRate = 0
		}
	}

	// show iteration count in "current/total" iterations format
	if c.showIterationsCount {
		if sb.Len() == 0 {
			sb.WriteString("(")
		} else {
			sb.WriteString(", ")
		}
		if !c.ignoreLength {
			if c.showBytes {
				currentHumanize, currentSuffix := humanizeBytes(s.currentBytes, c.useIECUnits)
				if currentSuffix == c.maxHumanizedSuffix {
					if c.showTotalBytes {
						sb.WriteString(fmt.Sprintf("%s/%s%s",
							currentHumanize, c.maxHumanized, c.maxHumanizedSuffix))
					} else {
						sb.WriteString(fmt.Sprintf("%s%s",
							currentHumanize, c.maxHumanizedSuffix))
					}
				} else if c.showTotalBytes {
					sb.WriteString(fmt.Sprintf("%s%s/%s%s",
						currentHumanize, currentSuffix, c.maxHumanized, c.maxHumanizedSuffix))
				} else {
					sb.WriteString(fmt.Sprintf("%s%s", currentHumanize, currentSuffix))
				}
			} else if c.showTotalBytes {
				sb.WriteString(fmt.Sprintf("%.0f/%d", s.currentBytes, c.max))
			} else {
				sb.WriteString(fmt.Sprintf("%.0f", s.currentBytes))
			}
		} else {
			if c.showBytes {
				currentHumanize, currentSuffix := humanizeBytes(s.currentBytes, c.useIECUnits)
				sb.WriteString(fmt.Sprintf("%s%s", currentHumanize, currentSuffix))
			} else if c.showTotalBytes {
				sb.WriteString(fmt.Sprintf("%.0f/%s", s.currentBytes, "-"))
			} else {
				sb.WriteString(fmt.Sprintf("%.0f", s.currentBytes))
			}
		}
	}

	// show rolling average rate
	if c.showBytes && averageRate > 0 && !math.IsInf(averageRate, 1) {
		if sb.Len() == 0 {
			sb.WriteString("(")
		} else {
			sb.WriteString(", ")
		}
		currentHumanize, currentSuffix := humanizeBytes(averageRate, c.useIECUnits)
		sb.WriteString(fmt.Sprintf("%s%s/s", currentHumanize, currentSuffix))
	}

	// show iterations rate
	if c.showIterationsPerSecond {
		if sb.Len() == 0 {
			sb.WriteString("(")
		} else {
			sb.WriteString(", ")
		}
		if averageRate > 1 {
			sb.WriteString(fmt.Sprintf("%0.0f %s/s", averageRate, c.iterationString))
		} else if averageRate*60 > 1 {
			sb.WriteString(fmt.Sprintf("%0.0f %s/min", 60*averageRate, c.iterationString))
		} else {
			sb.WriteString(fmt.Sprintf("%0.0f %s/hr", 3600*averageRate, c.iterationString))
		}
	}
	if sb.Len() > 0 {
		sb.WriteString(")")
	}

	leftBrac, rightBrac, saucer, saucerHead := "", "", "", ""
	barStart, barEnd := c.theme.BarStart, c.theme.BarEnd
	if s.finished && c.theme.BarEndFilled != "" {
		barEnd = c.theme.BarEndFilled
	}

	// show time prediction in "current/total" seconds format
	switch {
	case c.predictTime:
		rightBracNum := (time.Duration((1/averageRate)*(float64(c.max)-float64(s.currentNum))) * time.Second)
		if rightBracNum.Seconds() < 0 {
			rightBracNum = 0 * time.Second
		}
		rightBrac = rightBracNum.String()
		fallthrough
	case c.elapsedTime || c.showElapsedTimeOnFinish:
		leftBrac = (time.Duration(time.Since(s.startTime).Seconds()) * time.Second).String()
	}

	if c.fullWidth && !c.ignoreLength {
		width, err := termWidth()
		if err != nil {
			width = 80
		}

		amend := 1 // an extra space at eol
		switch {
		case leftBrac != "" && rightBrac != "":
			amend = 4 // space, square brackets and colon
		case leftBrac != "" && rightBrac == "":
			amend = 4 // space and square brackets and another space
		case leftBrac == "" && rightBrac != "":
			amend = 3 // space and square brackets
		}
		if c.showDescriptionAtLineEnd {
			amend += 1 // another space
		}

		c.width = width - getStringWidth(c, c.description, true) - 10 - amend - sb.Len() - len(leftBrac) - len(rightBrac)
		s.currentSaucerSize = int(float64(s.currentPercent) / 100.0 * float64(c.width))
	}
	if (s.currentSaucerSize > 0 || s.currentPercent > 0) && c.theme.BarStartFilled != "" {
		barStart = c.theme.BarStartFilled
	}
	if s.currentSaucerSize > 0 {
		if c.ignoreLength {
			saucer = strings.Repeat(c.theme.SaucerPadding, s.currentSaucerSize-1)
		} else {
			saucer = strings.Repeat(c.theme.Saucer, s.currentSaucerSize-1)
		}

		// Check if an alternate saucer head is set for animation
		if c.theme.AltSaucerHead != "" && s.isAltSaucerHead {
			saucerHead = c.theme.AltSaucerHead
			s.isAltSaucerHead = false
		} else if c.theme.SaucerHead == "" || s.currentSaucerSize == c.width {
			// use the saucer for the saucer head if it hasn't been set
			// to preserve backwards compatibility
			saucerHead = c.theme.Saucer
		} else {
			saucerHead = c.theme.SaucerHead
			s.isAltSaucerHead = true
		}
	}

	/*
		Progress Bar format
		Description % |------        |  (kb/s) (iteration count) (iteration rate) (predict time)

		or if showDescriptionAtLineEnd is enabled
		% |------        |  (kb/s) (iteration count) (iteration rate) (predict time) Description
	*/

	repeatAmount := c.width - s.currentSaucerSize
	if repeatAmount < 0 {
		repeatAmount = 0
	}

	str := ""

	if c.ignoreLength {
		selectedSpinner := spinners[c.spinnerType]
		if len(c.spinner) > 0 {
			selectedSpinner = c.spinner
		}

		var spinner string
		if c.spinnerChangeInterval != 0 {
			// if the spinner is changed according to an interval, calculate it
			spinner = selectedSpinner[int(math.Round(math.Mod(float64(time.Since(s.startTime).Nanoseconds()/c.spinnerChangeInterval.Nanoseconds()), float64(len(selectedSpinner)))))]
		} else {
			// if the spinner is changed according to the number render was called
			spinner = selectedSpinner[s.spinnerIdx]
			s.spinnerIdx = (s.spinnerIdx + 1) % len(selectedSpinner)
		}
		if c.elapsedTime {
			if c.showDescriptionAtLineEnd {
				str = fmt.Sprintf("\r%s %s [%s] %s ",
					spinner,
					sb.String(),
					leftBrac,
					c.description)
			} else {
				str = fmt.Sprintf("\r%s %s %s [%s] ",
					spinner,
					c.description,
					sb.String(),
					leftBrac)
			}
		} else {
			if c.showDescriptionAtLineEnd {
				str = fmt.Sprintf("\r%s %s %s ",
					spinner,
					sb.String(),
					c.description)
			} else {
				str = fmt.Sprintf("\r%s %s %s ",
					spinner,
					c.description,
					sb.String())
			}
		}
	} else if rightBrac == "" {
		str = fmt.Sprintf("%4d%% %s%s%s%s%s %s",
			s.currentPercent,
			barStart,
			saucer,
			saucerHead,
			strings.Repeat(c.theme.SaucerPadding, repeatAmount),
			barEnd,
			sb.String())
		if (s.currentPercent == 100 && c.showElapsedTimeOnFinish) || c.elapsedTime {
			str = fmt.Sprintf("%s [%s]", str, leftBrac)
		}

		if c.showDescriptionAtLineEnd {
			str = fmt.Sprintf("\r%s %s ", str, c.description)
		} else {
			str = fmt.Sprintf("\r%s%s ", c.description, str)
		}
	} else {
		if s.currentPercent == 100 {
			str = fmt.Sprintf("%4d%% %s%s%s%s%s %s",
				s.currentPercent,
				barStart,
				saucer,
				saucerHead,
				strings.Repeat(c.theme.SaucerPadding, repeatAmount),
				barEnd,
				sb.String())

			if c.showElapsedTimeOnFinish {
				str = fmt.Sprintf("%s [%s]", str, leftBrac)
			}

			if c.showDescriptionAtLineEnd {
				str = fmt.Sprintf("\r%s %s", str, c.description)
			} else {
				str = fmt.Sprintf("\r%s%s", c.description, str)
			}
		} else {
			str = fmt.Sprintf("%4d%% %s%s%s%s%s %s [%s:%s]",
				s.currentPercent,
				barStart,
				saucer,
				saucerHead,
				strings.Repeat(c.theme.SaucerPadding, repeatAmount),
				barEnd,
				sb.String(),
				leftBrac,
				rightBrac)

			if c.showDescriptionAtLineEnd {
				str = fmt.Sprintf("\r%s %s", str, c.description)
			} else {
				str = fmt.Sprintf("\r%s%s", c.description, str)
			}
		}
	}

	if c.colorCodes {
		// convert any color codes in the progress bar into the respective ANSI codes
		str = colorstring.Color(str)
	}

	s.rendered = str

	return getStringWidth(c, str, false), writeString(c, str)
}

func clearProgressBar(c config, s state) error {
	if s.maxLineWidth == 0 {
		return nil
	}
	if c.useANSICodes {
		// write the "clear current line" ANSI escape sequence
		return writeString(c, "\033[2K\r")
	}
	// fill the empty content
	// to overwrite the progress bar and jump
	// back to the beginning of the line
	str := fmt.Sprintf("\r%s\r", strings.Repeat(" ", s.maxLineWidth))
	return writeString(c, str)
	// the following does not show correctly if the previous line is longer than subsequent line
	// return writeString(c, "\r")
}

func writeString(c config, str string) error {
	if _, err := io.WriteString(c.writer, str); err != nil {
		return err
	}

	if f, ok := c.writer.(*os.File); ok {
		// ignore any errors in Sync(), as stdout
		// can't be synced on some operating systems
		// like Debian 9 (Stretch)
		f.Sync()
	}

	return nil
}

// Reader is the progressbar io.Reader struct
type Reader struct {
	io.Reader
	bar *ProgressBar
}

// NewReader return a new Reader with a given progress bar.
func NewReader(r io.Reader, bar *ProgressBar) Reader {
	return Reader{
		Reader: r,
		bar:    bar,
	}
}

// Read will read the data and add the number of bytes to the progressbar
func (r *Reader) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	r.bar.Add(n)
	return
}

// Close the reader when it implements io.Closer
func (r *Reader) Close() (err error) {
	if closer, ok := r.Reader.(io.Closer); ok {
		return closer.Close()
	}
	r.bar.Finish()
	return
}

// Write implement io.Writer
func (p *ProgressBar) Write(b []byte) (n int, err error) {
	n = len(b)
	err = p.Add(n)
	return
}

// Read implement io.Reader
func (p *ProgressBar) Read(b []byte) (n int, err error) {
	n = len(b)
	err = p.Add(n)
	return
}

func (p *ProgressBar) Close() (err error) {
	err = p.Finish()
	return
}

func average(xs []float64) float64 {
	total := 0.0
	for _, v := range xs {
		total += v
	}
	return total / float64(len(xs))
}

func humanizeBytes(s float64, iec bool) (string, string) {
	sizes := []string{" B", " kB", " MB", " GB", " TB", " PB", " EB"}
	base := 1000.0

	if iec {
		sizes = []string{" B", " KiB", " MiB", " GiB", " TiB", " PiB", " EiB"}
		base = 1024.0
	}

	if s < 10 {
		return fmt.Sprintf("%2.0f", s), sizes[0]
	}
	e := math.Floor(logn(float64(s), base))
	suffix := sizes[int(e)]
	val := math.Floor(float64(s)/math.Pow(base, e)*10+0.5) / 10
	f := "%.0f"
	if val < 10 {
		f = "%.1f"
	}

	return fmt.Sprintf(f, val), suffix
}

func logn(n, b float64) float64 {
	return math.Log(n) / math.Log(b)
}

// termWidth function returns the visible width of the current terminal
// and can be redefined for testing
var termWidth = func() (width int, err error) {
	width, _, err = term.GetSize(int(os.Stdout.Fd()))
	if err == nil {
		return width, nil
	}

	return 0, err
}

func shouldCacheOutput(pb *ProgressBar) bool {
	return !pb.state.finished && !pb.state.exit && !pb.config.invisible
}

func Bprintln(pb *ProgressBar, a ...interface{}) (int, error) {
	pb.lock.Lock()
	defer pb.lock.Unlock()
	if !shouldCacheOutput(pb) {
		return fmt.Fprintln(pb.config.writer, a...)
	} else {
		return fmt.Fprintln(&pb.config.stdBuffer, a...)
	}
}

func Bprintf(pb *ProgressBar, format string, a ...interface{}) (int, error) {
	pb.lock.Lock()
	defer pb.lock.Unlock()
	if !shouldCacheOutput(pb) {
		return fmt.Fprintf(pb.config.writer, format, a...)
	} else {
		return fmt.Fprintf(&pb.config.stdBuffer, format, a...)
	}
}
