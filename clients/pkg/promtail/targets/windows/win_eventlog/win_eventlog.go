// The MIT License (MIT)

// Copyright (c) 2015-2020 InfluxData Inc.

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
//+build windows

//revive:disable-next-line:var-naming
// Package win_eventlog Input plugin to collect Windows Event Log messages
package win_eventlog

import (
	"bytes"
	"encoding/xml"
	"path/filepath"
	"strings"
	"syscall"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/go-kit/kit/log/level"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
	"golang.org/x/sys/windows"
)

var sampleConfig = `
  ## Telegraf should have Administrator permissions to subscribe for some Windows Events channels
  ## (System log, for example)

  ## LCID (Locale ID) for event rendering
  ## 1033 to force English language
  ## 0 to use default Windows locale
  # locale = 0

  ## Name of eventlog, used only if xpath_query is empty
  ## Example: "Application"
  # eventlog_name = ""

  ## xpath_query can be in defined short form like "Event/System[EventID=999]"
  ## or you can form a XML Query. Refer to the Consuming Events article:
  ## https://docs.microsoft.com/en-us/windows/win32/wes/consuming-events
  ## XML query is the recommended form, because it is most flexible
  ## You can create or debug XML Query by creating Custom View in Windows Event Viewer
  ## and then copying resulting XML here
  xpath_query = '''
  <QueryList>
    <Query Id="0" Path="Security">
      <Select Path="Security">*</Select>
      <Suppress Path="Security">*[System[( (EventID &gt;= 5152 and EventID &lt;= 5158) or EventID=5379 or EventID=4672)]]</Suppress>
    </Query>
    <Query Id="1" Path="Application">
      <Select Path="Application">*[System[(Level &lt; 4)]]</Select>
    </Query>
    <Query Id="2" Path="Windows PowerShell">
      <Select Path="Windows PowerShell">*[System[(Level &lt; 4)]]</Select>
    </Query>
    <Query Id="3" Path="System">
      <Select Path="System">*</Select>
    </Query>
    <Query Id="4" Path="Setup">
      <Select Path="Setup">*</Select>
    </Query>
  </QueryList>
  '''

  ## System field names:
  ##   "Source", "EventID", "Version", "Level", "Task", "Opcode", "Keywords", "TimeCreated",
  ##   "EventRecordID", "ActivityID", "RelatedActivityID", "ProcessID", "ThreadID", "ProcessName",
  ##   "Channel", "Computer", "UserID", "UserName", "Message", "LevelText", "TaskText", "OpcodeText"

  ## In addition to System, Data fields can be unrolled from additional XML nodes in event.
  ## Human-readable representation of those nodes is formatted into event Message field,
  ## but XML is more machine-parsable

  # Process UserData XML to fields, if this node exists in Event XML
  process_userdata = true

  # Process EventData XML to fields, if this node exists in Event XML
  process_eventdata = true

  ## Separator character to use for unrolled XML Data field names
  separator = "_"

  ## Get only first line of Message field. For most events first line is usually more than enough
  only_first_line_of_message = true

  ## Parse timestamp from TimeCreated.SystemTime event field.
  ## Will default to current time of telegraf processing on parsing error or if set to false
  timestamp_from_event = true

  ## Fields to include as tags. Globbing supported ("Level*" for both "Level" and "LevelText")
  event_tags = ["Source", "EventID", "Level", "LevelText", "Task", "TaskText", "Opcode", "OpcodeText", "Keywords", "Channel", "Computer"]

  ## Default list of fields to send. All fields are sent by default. Globbing supported
  event_fields = ["*"]

  ## Fields to exclude. Also applied to data fields. Globbing supported
  exclude_fields = ["TimeCreated", "Binary", "Data_Address*"]

  ## Skip those tags or fields if their value is empty or equals to zero. Globbing supported
  exclude_empty = ["*ActivityID", "UserID"]
`

// WinEventLog config
type WinEventLog struct {
	Locale                 uint32   `yaml:"locale"`
	EventlogName           string   `yaml:"eventlog_name"`
	Query                  string   `yaml:"xpath_query"`
	ProcessUserData        bool     `yaml:"process_userdata"`
	ProcessEventData       bool     `yaml:"process_eventdata"`
	Separator              string   `yaml:"separator"`
	OnlyFirstLineOfMessage bool     `yaml:"only_first_line_of_message"`
	TimeStampFromEvent     bool     `yaml:"timestamp_from_event"`
	EventTags              []string `yaml:"event_tags"`
	EventFields            []string `yaml:"event_fields"`
	ExcludeFields          []string `yaml:"exclude_fields"`
	ExcludeEmpty           []string `yaml:"exclude_empty"`

	subscription EvtHandle
	buf          []byte
}

var bufferSize = 1 << 14

var description = "Input plugin to collect Windows Event Log messages"

// Description for win_eventlog
func (w *WinEventLog) Description() string {
	return description
}

// SampleConfig for win_eventlog
func (w *WinEventLog) SampleConfig() string {
	return sampleConfig
}

// Gather Windows Event Log entries
func (w *WinEventLog) Gather(acc telegraf.Accumulator) error {

	// var err error
	// if w.subscription == 0 {
	// 	w.subscription, err = w.evtSubscribe(w.EventlogName, w.Query)
	// 	if err != nil {
	// 		return fmt.Errorf("Windows Event Log subscription error: %v", err.Error())
	// 	}
	// }

	// loop:
	// 	for {
	// 		events, err := w.FetchEvents(w.subscription)
	// 		if err != nil {
	// 			switch {
	// 			case err == ERROR_NO_MORE_ITEMS:
	// 				break loop
	// 			case err != nil:
	// 				// w.Log.Error("Error getting events:", err.Error())
	// 				return err
	// 			}
	// 		}

	// 		for _, event := range events {
	// 			// Prepare fields names usage counter
	// 			var fieldsUsage = map[string]int{}

	// 			tags := map[string]string{}
	// 			fields := map[string]interface{}{}
	// 			evt := reflect.ValueOf(&event).Elem()
	// 			timeStamp := time.Now()
	// 			// Walk through all fields of Event struct to process System tags or fields
	// 			for i := 0; i < evt.NumField(); i++ {
	// 				fieldName := evt.Type().Field(i).Name
	// 				fieldType := evt.Field(i).Type().String()
	// 				fieldValue := evt.Field(i).Interface()
	// 				computedValues := map[string]interface{}{}
	// 				switch fieldName {
	// 				case "Source":
	// 					fieldValue = event.Source.Name
	// 					fieldType = reflect.TypeOf(fieldValue).String()
	// 				case "Execution":
	// 					fieldValue := event.Execution.ProcessID
	// 					fieldType = reflect.TypeOf(fieldValue).String()
	// 					fieldName = "ProcessID"
	// 					// Look up Process Name from pid
	// 					if should, _ := w.shouldProcessField("ProcessName"); should {
	// 						_, _, processName, err := GetFromSnapProcess(fieldValue)
	// 						if err == nil {
	// 							computedValues["ProcessName"] = processName
	// 						}
	// 					}
	// 				case "TimeCreated":
	// 					fieldValue = event.TimeCreated.SystemTime
	// 					fieldType = reflect.TypeOf(fieldValue).String()
	// 					if w.TimeStampFromEvent {
	// 						timeStamp, err = time.Parse(time.RFC3339Nano, fmt.Sprintf("%v", fieldValue))
	// 						if err != nil {
	// 							// w.Log.Warnf("Error parsing timestamp %q: %v", fieldValue, err)
	// 						}
	// 					}
	// 				case "Correlation":
	// 					if should, _ := w.shouldProcessField("ActivityID"); should {
	// 						activityID := event.Correlation.ActivityID
	// 						if len(activityID) > 0 {
	// 							computedValues["ActivityID"] = activityID
	// 						}
	// 					}
	// 					if should, _ := w.shouldProcessField("RelatedActivityID"); should {
	// 						relatedActivityID := event.Correlation.RelatedActivityID
	// 						if len(relatedActivityID) > 0 {
	// 							computedValues["RelatedActivityID"] = relatedActivityID
	// 						}
	// 					}
	// 				case "Security":
	// 					computedValues["UserID"] = event.Security.UserID
	// 					// Look up UserName and Domain from SID
	// 					if should, _ := w.shouldProcessField("UserName"); should {
	// 						sid := event.Security.UserID
	// 						usid, err := syscall.StringToSid(sid)
	// 						if err == nil {
	// 							username, domain, _, err := usid.LookupAccount("")
	// 							if err == nil {
	// 								computedValues["UserName"] = fmt.Sprint(domain, "\\", username)
	// 							}
	// 						}
	// 					}
	// 				default:
	// 				}
	// 				if should, where := w.shouldProcessField(fieldName); should {
	// 					if where == "tags" {
	// 						strValue := fmt.Sprintf("%v", fieldValue)
	// 						if !w.shouldExcludeEmptyField(fieldName, "string", strValue) {
	// 							tags[fieldName] = strValue
	// 							fieldsUsage[fieldName]++
	// 						}
	// 					} else if where == "fields" {
	// 						if !w.shouldExcludeEmptyField(fieldName, fieldType, fieldValue) {
	// 							fields[fieldName] = fieldValue
	// 							fieldsUsage[fieldName]++
	// 						}
	// 					}
	// 				}

	// 				// Insert computed fields
	// 				for computedKey, computedValue := range computedValues {
	// 					if should, where := w.shouldProcessField(computedKey); should {
	// 						if where == "tags" {
	// 							tags[computedKey] = fmt.Sprintf("%v", computedValue)
	// 							fieldsUsage[computedKey]++
	// 						} else if where == "fields" {
	// 							fields[computedKey] = computedValue
	// 							fieldsUsage[computedKey]++
	// 						}
	// 					}
	// 				}
	// 			}

	// 			// Unroll additional XML
	// 			var xmlFields []EventField
	// 			if w.ProcessUserData {
	// 				fieldsUserData, xmlFieldsUsage := UnrollXMLFields(event.UserData.InnerXML, fieldsUsage, w.Separator)
	// 				xmlFields = append(xmlFields, fieldsUserData...)
	// 				fieldsUsage = xmlFieldsUsage
	// 			}
	// 			if w.ProcessEventData {
	// 				fieldsEventData, xmlFieldsUsage := UnrollXMLFields(event.EventData.InnerXML, fieldsUsage, w.Separator)
	// 				xmlFields = append(xmlFields, fieldsEventData...)
	// 				fieldsUsage = xmlFieldsUsage
	// 			}
	// 			uniqueXMLFields := UniqueFieldNames(xmlFields, fieldsUsage, w.Separator)
	// 			for _, xmlField := range uniqueXMLFields {
	// 				if !w.shouldExclude(xmlField.Name) {
	// 					fields[xmlField.Name] = xmlField.Value
	// 				}
	// 			}

	// 			// Pass collected metrics
	// 			acc.AddFields("win_eventlog", fields, tags, timeStamp)
	// 		}
	// 	}

	return nil
}

func (w *WinEventLog) shouldExclude(field string) (should bool) {
	for _, excludePattern := range w.ExcludeFields {
		// Check if field name matches excluded list
		if matched, _ := filepath.Match(excludePattern, field); matched {
			return true
		}
	}
	return false
}

func (w *WinEventLog) shouldProcessField(field string) (should bool, list string) {
	for _, pattern := range w.EventTags {
		if matched, _ := filepath.Match(pattern, field); matched {
			// Tags are not excluded
			return true, "tags"
		}
	}

	for _, pattern := range w.EventFields {
		if matched, _ := filepath.Match(pattern, field); matched {
			if w.shouldExclude(field) {
				return false, "excluded"
			}
			return true, "fields"
		}
	}
	return false, "excluded"
}

func (w *WinEventLog) shouldExcludeEmptyField(field string, fieldType string, fieldValue interface{}) (should bool) {
	for _, pattern := range w.ExcludeEmpty {
		if matched, _ := filepath.Match(pattern, field); matched {
			switch fieldType {
			case "string":
				return len(fieldValue.(string)) < 1
			case "int":
				return fieldValue.(int) == 0
			case "uint32":
				return fieldValue.(uint32) == 0
			}
		}
	}
	return false
}

func EvtSubscribe(logName, xquery string) (EvtHandle, error) {
	var logNamePtr, xqueryPtr *uint16

	sigEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(sigEvent)

	logNamePtr, err = syscall.UTF16PtrFromString(logName)
	if err != nil {
		return 0, err
	}

	xqueryPtr, err = syscall.UTF16PtrFromString(xquery)
	if err != nil {
		return 0, err
	}

	subsHandle, err := _EvtSubscribe(0, uintptr(sigEvent), logNamePtr, xqueryPtr,
		0, 0, 0, EvtSubscribeToFutureEvents)
	if err != nil {
		return 0, err
	}
	level.Debug(util_log.Logger).Log("msg", "Subcribed with handle id", "id", subsHandle)

	return subsHandle, nil
}

func EvtSubscribeWithBookmark(logName, xquery string, bookMark EvtHandle) (EvtHandle, error) {
	var logNamePtr, xqueryPtr *uint16

	sigEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(sigEvent)

	logNamePtr, err = syscall.UTF16PtrFromString(logName)
	if err != nil {
		return 0, err
	}

	xqueryPtr, err = syscall.UTF16PtrFromString(xquery)
	if err != nil {
		return 0, err
	}

	subsHandle, err := _EvtSubscribe(0, uintptr(sigEvent), logNamePtr, xqueryPtr,
		bookMark, 0, 0, EvtSubscribeStartAfterBookmark)
	if err != nil {
		return 0, err
	}
	level.Debug(util_log.Logger).Log("msg", "Subcribed with handle id", "id", subsHandle)

	return subsHandle, nil
}

func fetchEventHandles(subsHandle EvtHandle) ([]EvtHandle, error) {
	var eventsNumber uint32
	var evtReturned uint32

	eventsNumber = 5

	eventHandles := make([]EvtHandle, eventsNumber)

	err := _EvtNext(subsHandle, eventsNumber, &eventHandles[0], 0, 0, &evtReturned)
	if err != nil {
		if err == ERROR_INVALID_OPERATION && evtReturned == 0 {
			return nil, ERROR_NO_MORE_ITEMS
		}
		return nil, err
	}

	return eventHandles[:evtReturned], nil
}

type EventFetcher struct {
	buf []byte
}

func NewEventFetcher() *EventFetcher {
	return &EventFetcher{}
}

func (w *EventFetcher) FetchEvents(subsHandle EvtHandle, lang uint32) ([]Event, []EvtHandle, error) {
	if w.buf == nil {
		w.buf = make([]byte, bufferSize)
	}
	var events []Event

	eventHandles, err := fetchEventHandles(subsHandle)
	if err != nil {
		return nil, nil, err
	}

	for _, eventHandle := range eventHandles {
		if eventHandle != 0 {
			event, err := w.renderEvent(eventHandle, lang)
			if err == nil {
				events = append(events, event)
			}
		}
	}

	return events, eventHandles, nil
}

func Close(handles []EvtHandle) error {
	for i := 0; i < len(handles); i++ {
		err := _EvtClose(handles[i])
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *EventFetcher) renderEvent(eventHandle EvtHandle, lang uint32) (Event, error) {
	var bufferUsed, propertyCount uint32

	event := Event{}
	err := _EvtRender(0, eventHandle, EvtRenderEventXml, uint32(len(w.buf)), &w.buf[0], &bufferUsed, &propertyCount)
	if err != nil {
		return event, err
	}

	eventXML, err := DecodeUTF16(w.buf[:bufferUsed])
	if err != nil {
		return event, err
	}
	err = xml.Unmarshal([]byte(eventXML), &event)
	if err != nil {
		// We can return event without most text values,
		// that way we will not loose information
		// This can happen when processing Forwarded Events
		return event, nil
	}

	publisherHandle, err := openPublisherMetadata(0, event.Source.Name, lang)
	if err != nil {
		return event, nil
	}
	defer _EvtClose(publisherHandle)

	// Populating text values
	keywords, err := formatEventString(EvtFormatMessageKeyword, eventHandle, publisherHandle)
	if err == nil {
		event.Keywords = keywords
	}
	message, err := formatEventString(EvtFormatMessageEvent, eventHandle, publisherHandle)
	if err == nil {
		event.Message = message
	}
	level, err := formatEventString(EvtFormatMessageLevel, eventHandle, publisherHandle)
	if err == nil {
		event.LevelText = level
	}
	task, err := formatEventString(EvtFormatMessageTask, eventHandle, publisherHandle)
	if err == nil {
		event.TaskText = task
	}
	opcode, err := formatEventString(EvtFormatMessageOpcode, eventHandle, publisherHandle)
	if err == nil {
		event.OpcodeText = opcode
	}
	return event, nil
}

func formatEventString(
	messageFlag EvtFormatMessageFlag,
	eventHandle EvtHandle,
	publisherHandle EvtHandle,
) (string, error) {
	var bufferUsed uint32
	err := _EvtFormatMessage(publisherHandle, eventHandle, 0, 0, 0, messageFlag,
		0, nil, &bufferUsed)
	if err != nil && err != ERROR_INSUFFICIENT_BUFFER {
		return "", err
	}

	bufferUsed *= 2
	buffer := make([]byte, bufferUsed)
	bufferUsed = 0

	err = _EvtFormatMessage(publisherHandle, eventHandle, 0, 0, 0, messageFlag,
		uint32(len(buffer)/2), &buffer[0], &bufferUsed)
	bufferUsed *= 2
	if err != nil {
		return "", err
	}

	result, err := DecodeUTF16(buffer[:bufferUsed])
	if err != nil {
		return "", err
	}

	var out string
	if messageFlag == EvtFormatMessageKeyword {
		// Keywords are returned as array of a zero-terminated strings
		splitZero := func(c rune) bool { return c == '\x00' }
		eventKeywords := strings.FieldsFunc(string(result), splitZero)
		// So convert them to comma-separated string
		out = strings.Join(eventKeywords, ",")
	} else {
		result := bytes.Trim(result, "\x00")
		out = string(result)
	}
	return out, nil
}

// openPublisherMetadata opens a handle to the publisher's metadata. Close must
// be called on returned EvtHandle when finished with the handle.
func openPublisherMetadata(
	session EvtHandle,
	publisherName string,
	lang uint32,
) (EvtHandle, error) {
	p, err := syscall.UTF16PtrFromString(publisherName)
	if err != nil {
		return 0, err
	}

	h, err := _EvtOpenPublisherMetadata(session, p, nil, lang, 0)
	if err != nil {
		return 0, err
	}

	return h, nil
}

func init() {
	inputs.Add("win_eventlog", func() telegraf.Input {
		return &WinEventLog{
			buf:                    make([]byte, bufferSize),
			ProcessUserData:        true,
			ProcessEventData:       true,
			Separator:              "_",
			OnlyFirstLineOfMessage: true,
			TimeStampFromEvent:     true,
			EventTags:              []string{"Source", "EventID", "Level", "LevelText", "Keywords", "Channel", "Computer"},
			EventFields:            []string{"*"},
			ExcludeEmpty:           []string{"Task", "Opcode", "*ActivityID", "UserID"},
		}
	})
}
