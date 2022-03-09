# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [3.1.0] - 2019-07-15

### Added

- Added Tree widget [#237]

## [3.0.0] - 2019-03-07

### Changed

- Added sync.Locker interface to Drawable interface

## 2019-03-01

### Changed

- Change scroll method names in List widget

### Fixed

- Fix List widget scrolling

## 2019-02-28

### Added

- Add `ColumnResizer` to table which allows for custom column sizing
- Add widget padding

### Changed

- Change various widget field names
- s/`TextParse`/`ParseStyles`
- Remove `AddColorMap` in place of modifying `StyleParserColorMap` directly

## 2019-01-31

### Added

- Add more scrolling options to List

### Changed

- Make list scroll automatically

### Added

## 2019-01-26

### Added

- Add scrolling to List widget
- Add WrapText option to Paragraph
  - controls if text should wrap automatically

## 2019-01-24

### Added

- Add image widget [#126]

### Changed

- Change LineChart to Plot
  - Added ScatterPlot mode which plots points instead of lines between points

## 2019-01-23

### Added

- Add `Canvas` which allows for drawing braille lines to a `Buffer`

### Changed

- Set `termbox-go` backend to 256 colors by default
- Moved widgets to `github.com/gizak/termui/widgets`
- Rewrote widgets (check examples and code)
- Rewrote grid
  - grids are instantiated locally instead of through `termui.Body`
  - grids can be nested
  - change grid layout mechanism
    - columns and rows can be arbitrarily nested
    - column and row size is now specified as a ratio of the available space
- `Cell`s now contain a `Style` which holds a `Fg`, `Bg`, and `Modifier`
- Change `Bufferer` interface to `Drawable`
  - Add `GetRect` and `SetRect` methods to control widget sizing
  - Change `Buffer` method to `Draw`
    - `Draw` takes a `Buffer` and draws to it instead of returning a new `Buffer`
- Refactor `Theme`
  - `Theme` is now a large struct which holds the default `Styles` of everything
- Combine `TermWidth` and `TermHeight` functions into `TerminalDimensions`
- Rework `Block`
- Rework `Buffer` methods
- Decremente color numbers by 1 to match xterm colors
- Change text parsing
  - change style items from `fg-color` to `fg:color`
  - adde mod item like `mod:reverse`

## 2018-11-29

### Changed

- Move Tabpane from termui/extra to termui and rename it to TabPane
- Rename PollEvent to PollEvents

## 2018-11-28

### Changed

- Migrate from Dep to vgo
- Overhaul the event system
  - check the wiki/examples for details
- Rename Par widget to Paragraph
- Rename MBarChart widget to StackedBarChart

[#237]: https://github.com/gizak/termui/pull/237
[#126]: https://github.com/gizak/termui/pull/126

[Unreleased]: https://github.com/gizak/termui/compare/v3.1.0...HEAD
[3.1.0]: https://github.com/gizak/termui/compare/v3.0.0...v3.1.0
[3.0.0]: https://github.com/gizak/termui/compare/v2.3.0...v3.0.0
