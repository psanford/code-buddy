package interactive

import (
	"bufio"
	"reflect"
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {

	commandPrefix = "#challenges-forsakes"

	tests := []struct {
		name    string
		input   string
		want    *FunctionCall
		wantErr bool
	}{
		{
			name: "Valid function call with parameters",
			input: `#challenges-forsakes,function,test_function
#challenges-forsakes,parameter,param1
This is the content of param1
#challenges-forsakes,end_parameter
#challenges-forsakes,parameter,param2

This is the content of param2

#challenges-forsakes,end_parameter
#challenges-forsakes,end_function`,
			want: &FunctionCall{
				Name: "test_function",
				Parameters: []FunctionParameter{
					{Name: "param1", Value: "This is the content of param1"},
					{Name: "param2", Value: "\nThis is the content of param2\n"},
				},
			},
			wantErr: false,
		},
		{
			name: "Valid function call with parameters but no end function",
			input: `#challenges-forsakes,function,test_function
#challenges-forsakes,parameter,param1
This is the content of param1
#challenges-forsakes,end_parameter
#challenges-forsakes,parameter,param2
This is the content of param2
#challenges-forsakes,end_parameter`,
			want: &FunctionCall{
				Name: "test_function",
				Parameters: []FunctionParameter{
					{Name: "param1", Value: "This is the content of param1"},
					{Name: "param2", Value: "This is the content of param2"},
				},
			},
			wantErr: false,
		},
		{
			name:    "Invalid function call - missing end_function",
			input:   "#challenges-forsakes,function,test_function",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Invalid function call - wrong command",
			input:   "#challenges-forsakes,invalid_command,test_function",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCommand(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCmdParser_consumeUntilPrefix(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantBefore string
		wantParts  []string
		wantErr    bool
	}{
		{
			name:       "Valid input",
			input:      "Line 1\nLine 2\n#challenges-forsakes,function,test",
			wantBefore: "Line 1\nLine 2",
			wantParts:  []string{"#challenges-forsakes", "function", "test"},
			wantErr:    false,
		},
		{
			name:       "No prefix found",
			input:      "Line 1\nLine 2\n",
			wantBefore: "Line 1\nLine 2",
			wantParts:  nil,
			wantErr:    true,
		},
		{
			name:       "Invalid function call line",
			input:      "Line 1\n#challenges-forsakes\n",
			wantBefore: "Line 1",
			wantParts:  nil,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &cmdParser{
				scanner: bufio.NewScanner(strings.NewReader(tt.input)),
			}
			gotBefore, gotParts, err := c.consumeUntilPrefix()
			if (err != nil) != tt.wantErr {
				t.Errorf("cmdParser.consumeUntilPrefix() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotBefore != tt.wantBefore {
				t.Errorf("cmdParser.consumeUntilPrefix() gotBefore = %v, want %v", gotBefore, tt.wantBefore)
			}
			if !reflect.DeepEqual(gotParts, tt.wantParts) {
				t.Errorf("cmdParser.consumeUntilPrefix() gotParts = %v, want %v", gotParts, tt.wantParts)
			}
		})
	}
}

func TestCmdParser_consumeFuncCall(t *testing.T) {
	commandPrefix = "#challenges-forsakes"

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:    "Valid function call",
			input:   "#challenges-forsakes,function,test_function",
			want:    "test_function",
			wantErr: false,
		},
		{
			name:    "Invalid command",
			input:   "#challenges-forsakes,invalid,test_function",
			want:    "",
			wantErr: true,
		},
		{
			name:    "Invalid number of parts",
			input:   "#challenges-forsakes,function",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &cmdParser{
				scanner: bufio.NewScanner(strings.NewReader(tt.input)),
			}
			got, err := c.consumeFuncCall()
			if (err != nil) != tt.wantErr {
				t.Errorf("cmdParser.consumeFuncCall() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("cmdParser.consumeFuncCall() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCmdParser_consumeParams(t *testing.T) {
	commandPrefix = "#challenges-forsakes"

	tests := []struct {
		name    string
		input   string
		want    []FunctionParameter
		wantErr bool
	}{
		{
			name: "Valid parameters",
			input: `#challenges-forsakes,parameter,param1
Content 1
#challenges-forsakes,end_parameter
#challenges-forsakes,parameter,param2
Content 2
#challenges-forsakes,end_parameter
#challenges-forsakes,end_function`,
			want: []FunctionParameter{
				{Name: "param1", Value: "Content 1"},
				{Name: "param2", Value: "Content 2"},
			},
			wantErr: false,
		},
		{
			name: "Invalid parameter - missing end_parameter",
			input: `#challenges-forsakes,parameter,param1
Content 1
#challenges-forsakes,end_function`,
			want:    nil,
			wantErr: true,
		},
		{
			name: "Invalid command",
			input: `#challenges-forsakes,invalid,param1
Content 1
#challenges-forsakes,end_parameter`,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &cmdParser{
				scanner: bufio.NewScanner(strings.NewReader(tt.input)),
			}
			got, err := c.consumeParams()
			if (err != nil) != tt.wantErr {
				t.Errorf("cmdParser.consumeParams() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("cmdParser.consumeParams() = %v, want %v", got, tt.want)
			}
		})
	}
}
