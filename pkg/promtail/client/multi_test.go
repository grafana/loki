package client

import (
	"reflect"
	"testing"

	"github.com/go-kit/kit/log"
)

func TestNewMulti(t *testing.T) {
	type args struct {
		logger log.Logger
		cfgs   []Config
	}
	tests := []struct {
		name    string
		args    args
		want    MultiClient
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewMulti(tt.args.logger, tt.args.cfgs...)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewMulti() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewMulti() = %v, want %v", got, tt.want)
			}
		})
	}
}
