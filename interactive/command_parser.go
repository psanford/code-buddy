package interactive

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
)

var commandPrefix = reverseString("function_call#")

type cmdParser struct {
	scanner *bufio.Scanner
	err     error
}

func (c *cmdParser) consumeUntilPrefix() (string, []string, error) {
	var beforeText []string
	for c.scanner.Scan() {
		line := c.scanner.Text()
		if strings.HasPrefix(line, commandPrefix) {
			parts := strings.Split(line, ",")

			if len(parts) < 2 {
				c.err = fmt.Errorf("invalid function call line: %s", line)
				return strings.Join(beforeText, "\n"), nil, c.err
			}

			return strings.Join(beforeText, "\n"), parts, nil
		} else {
			beforeText = append(beforeText, line)
		}
	}
	c.err = io.EOF
	return strings.Join(beforeText, "\n"), nil, c.err
}

func (c *cmdParser) consumeFuncCall() (string, error) {
	if c.err != nil {
		return "", c.err
	}
	_, cmdParts, err := c.consumeUntilPrefix()
	if err != nil {
		return "", err
	}

	cmd := strings.TrimSpace(cmdParts[1])

	if cmd != "function" {
		c.err = fmt.Errorf("cmd parse err: expected function got %s", cmd)
		return "", c.err
	}

	if len(cmdParts) != 3 {
		c.err = fmt.Errorf("cmd parse err: function call wrong shape %s != 3 parts", strings.Join(cmdParts, ","))
		return "", c.err
	}

	name := strings.TrimSpace(cmdParts[2])
	return name, nil
}

func (c *cmdParser) consumeParams() ([]FunctionParameter, error) {
	var params []FunctionParameter
	for {
		preText, cmdParts, err := c.consumeUntilPrefix()
		if err != nil {
			return nil, err
		}
		if preText != "" {
			c.err = fmt.Errorf("got unexpected text within command: %s", preText)
			return nil, c.err
		}

		cmd := cmdParts[1]
		switch cmd {
		case "parameter":
			if len(cmdParts) != 3 {
				c.err = fmt.Errorf("parameter parse err: wrong shape %s != 3 parts", strings.Join(cmdParts, ","))
				return nil, c.err
			}

			paramName := cmdParts[2]

			paramBody, cmdParts, err := c.consumeUntilPrefix()
			if err != nil {
				return nil, err
			}
			if cmdParts[1] != "end_parameter" {
				c.err = fmt.Errorf("parameter parse err: parameter not terminated %s: got=%s", paramName, strings.Join(cmdParts, ","))
				return nil, c.err
			}

			params = append(params, FunctionParameter{
				Name:  paramName,
				Value: paramBody,
			})

		case "end_function":
			return params, nil
		default:
			c.err = fmt.Errorf("paramter parse err: got unexpected line, expecting parameter or end_function, got: %s", strings.Join(cmdParts, ","))
		}
	}
}

func (c *cmdParser) Parse() (*FunctionCall, error) {
	funName, err := c.consumeFuncCall()
	if err != nil {
		return nil, err
	}
	params, err := c.consumeParams()
	if err != nil {
		return nil, err
	}

	fc := FunctionCall{
		Name:       funName,
		Parameters: params,
	}

	return &fc, nil
}

func parseCommand(text string) (*FunctionCall, string, error) {
	funcEndTxt := commandPrefix + ",end_function"
	endIdx := strings.Index(text, funcEndTxt)

	if endIdx < 0 {
		return nil, text, io.EOF
	}

	scanner := bufio.NewScanner(bytes.NewBufferString(text))

	p := &cmdParser{
		scanner: scanner,
	}

	fc, err := p.Parse()

	fixedText := text[:endIdx+len(funcEndTxt)] + "\n" + commandPrefix + ",invoke\n"

	return fc, fixedText, err
}
