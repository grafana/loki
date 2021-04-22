//+build windows

package windows

import (
	"fmt"
	"syscall"

	jsoniter "github.com/json-iterator/go"

	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/windows/win_eventlog"
)

type Event struct {
	Source   string `json:"source,omitempty"`
	Channel  string `json:"channel,omitempty"`
	Computer string `json:"computer,omitempty"`
	EventID  int    `json:"event_id,omitempty"`
	Version  int    `json:"version,omitempty"`

	Level  int `json:"level,omitempty"`
	Task   int `json:"task,omitempty"`
	Opcode int `json:"opCode,omitempty"`

	LevelText  string `json:"levelText,omitempty"`
	TaskText   string `json:"taskText,omitempty"`
	OpcodeText string `json:"opCodeText,omitempty"`

	Keywords      string       `json:"keywords,omitempty"`
	TimeCreated   string       `json:"timeCreated,omitempty"`
	EventRecordID int          `json:"eventRecordID,omitempty"`
	Correlation   *Correlation `json:"correlation,omitempty"`
	Execution     *Execution   `json:"execution,omitempty"`

	Security  *Security `json:"security,omitempty"`
	UserData  string    `json:"user_data,omitempty"`
	EventData string    `json:"event_data,omitempty"`
	Message   string    `json:"message,omitempty"`
}

type Security struct {
	UserID   string `json:"userId,omitempty"`
	UserName string `json:"userName,omitempty"`
}

type Execution struct {
	ProcessID   uint32 `json:"processId,omitempty"`
	ThreadID    uint32 `json:"threadId,omitempty"`
	ProcessName string `json:"processName,omitempty"`
}

type Correlation struct {
	ActivityID        string `json:"activityID,omitempty"`
	RelatedActivityID string `json:"relatedActivityID,omitempty"`
}

// formatLine format a Loki log line from a windows event.
func formatLine(cfg *scrapeconfig.WindowsEventsTargetConfig, event win_eventlog.Event) (string, error) {
	structuredEvent := Event{
		Source:        event.Source.Name,
		Channel:       event.Channel,
		Computer:      event.Computer,
		EventID:       event.EventID,
		Version:       event.Version,
		Level:         event.Level,
		Task:          event.Task,
		Opcode:        event.Opcode,
		LevelText:     event.LevelText,
		TaskText:      event.TaskText,
		OpcodeText:    event.OpcodeText,
		Keywords:      event.Keywords,
		TimeCreated:   event.TimeCreated.SystemTime,
		EventRecordID: event.EventRecordID,
		Message:       event.Message,
	}

	if !cfg.ExcludeEventData {
		structuredEvent.EventData = string(event.EventData.InnerXML)
	}
	if !cfg.ExcludeUserData {
		structuredEvent.UserData = string(event.EventData.InnerXML)
	}
	if event.Correlation.ActivityID != "" || event.Correlation.RelatedActivityID != "" {
		structuredEvent.Correlation = &Correlation{
			ActivityID:        event.Correlation.ActivityID,
			RelatedActivityID: event.Correlation.RelatedActivityID,
		}
	}
	// best effort to get the username of the event.
	if event.Security.UserID != "" {
		var userName string
		usid, err := syscall.StringToSid(event.Security.UserID)
		if err == nil {
			username, domain, _, err := usid.LookupAccount("")
			if err == nil {
				userName = fmt.Sprint(domain, "\\", username)
			}
		}
		structuredEvent.Security = &Security{
			UserID:   event.Security.UserID,
			UserName: userName,
		}
	}
	if event.Execution.ProcessID != 0 {
		structuredEvent.Execution = &Execution{
			ProcessID: event.Execution.ProcessID,
			ThreadID:  event.Execution.ThreadID,
		}
		_, _, processName, err := win_eventlog.GetFromSnapProcess(event.Execution.ProcessID)
		if err == nil {
			structuredEvent.Execution.ProcessName = processName
		}
	}
	return jsoniter.MarshalToString(structuredEvent)
}
