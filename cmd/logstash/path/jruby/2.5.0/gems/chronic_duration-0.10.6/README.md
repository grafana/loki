[![Build Status](https://travis-ci.org/hpoydar/chronic_duration.png?branch=master)](https://travis-ci.org/hpoydar/chronic_duration)

# Chronic Duration

A simple Ruby natural language parser for elapsed time. (For example, 4 hours and 30 minutes, 6 minutes 4 seconds, 3 days, etc.) Returns all results in seconds. Will return an integer unless you get tricky and need a float. (4 minutes and 13.47 seconds, for example.)

The reverse can also be accomplished with the output method. So pass in seconds and you can get strings like 4 mins 31.51 secs (default  format), 4h 3m 30s, or 4:01:29.

## Usage

    >> require 'chronic_duration'
    => true
    >> ChronicDuration.parse('4 minutes and 30 seconds')
    => 270
    >> ChronicDuration.parse('0 seconds')
    => nil
    >> ChronicDuration.parse('0 seconds', :keep_zero => true)
    => 0
    >> ChronicDuration.output(270)
    => 4 mins 30 secs
    >> ChronicDuration.output(0)
    => nil
    >> ChronicDuration.output(0, :keep_zero => true)
    => 0 secs
    >> ChronicDuration.output(270, :format => :short)
    => 4m 30s
    >> ChronicDuration.output(270, :format => :long)
    => 4 minutes 30 seconds
    >> ChronicDuration.output(270, :format => :chrono)
    => 4:30
    >> ChronicDuration.output(1299600, :weeks => true)
    => 2 wks 1 day 1 hr
    >> ChronicDuration.output(1299600, :weeks => true, :units => 2)
    => 2 wks 1 day
    >> ChronicDuration.output(45*24*60*60 + 15*60, :limit_to_hours => true)
    => 1080 hrs 15 mins
    >> ChronicDuration.output(1299600, :weeks => true, :units => 2, :joiner => ', ')
    => 2 wks, 1 day
    >> ChronicDuration.output(1296000)
    => 15 days

Nil is returned if the string can't be parsed

Examples of parse-able strings:

* '12.4 secs'
* '1:20'
* '1:20.51'
* '4:01:01'
* '3 mins 4 sec'
* '2 hrs 20 min'
* '2h20min'
* '6 mos 1 day'
* '47 yrs 6 mos and 4d'
* 'two hours and twenty minutes'
* '3 weeks and 2 days'

ChronicDuration.raise_exceptions can be set to true to raise exceptions when the string can't be parsed.

    >> ChronicDuration.raise_exceptions = true
    => true
    >> ChronicDuration.parse('4 elephants and 3 Astroids')
    ChronicDuration::DurationParseError: An invalid word "elephants" was used in the string to be parsed.

## Contributing

Fork and pull request after your specs are green. Add your handle to the list below.
Also looking for additional maintainers.

## Contributors

errm,pdf, brianjlandau, jduff, olauzon, roboman, ianlevesque, bolandrm

## TODO

* Benchmark, optimize
* Context specific matching (E.g., for '4m30s', assume 'm' is minutes not months)
* Smartly parse vacation-like durations (E.g., '4 days and 3 nights')
* :chrono output option should probably change to something like 4 days 4:00:12 instead of 4:04:00:12
* Other locale support
