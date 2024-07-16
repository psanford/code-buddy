package interactive

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"unicode/utf8"
)

type ListFilesArgs struct {
	Pattern string `json:"pattern"`
}

func (a *ListFilesArgs) PrettyCommand() string {
	return fmt.Sprintf("rg --files | rg %s", a.Pattern)
}

var cmdCombinedOutput = func(name string, arg ...string) ([]byte, error) {
	return exec.Command(name, arg...).CombinedOutput()
}

func (a *ListFilesArgs) Run() (string, error) {
	regx, err := regexp.Compile("(?m)" + a.Pattern)
	if err != nil {
		return "", err
	}

	cmdOut, err := cmdCombinedOutput("rg", "--files")
	if err != nil {
		return "", err
	}

	var outBuf bytes.Buffer
	r := bufio.NewReader(bytes.NewBuffer(cmdOut))
	for {
		line, err := r.ReadBytes('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			return "", err
		}

		if regx.Match(line) {
			outBuf.Write(line)
		}
	}

	return outBuf.String(), nil
}

type RGArgs struct {
	Pattern   string `json:"pattern"`
	Directory string `json:"directory"`
}

func (a *RGArgs) PrettyCommand() string {
	return fmt.Sprintf("rg %s %s", a.Pattern, a.Directory)
}

func (a *RGArgs) Run() (string, error) {
	cmdOut, err := cmdCombinedOutput("rg", a.Pattern, a.Directory)
	if err != nil {
		return fmt.Sprintf("cmd err:%s output:%s", err, cmdOut), nil
	}

	return string(cmdOut), nil
}

type CatArgs struct {
	Filename string `json:"filename"`
}

func (a *CatArgs) Run() (string, error) {
	b, err := os.ReadFile(a.Filename)
	return string(b), err
}

func (a *CatArgs) PrettyCommand() string {
	return fmt.Sprintf("cat %s", a.Filename)
}

type ModifyFileArgs struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

func (a *ModifyFileArgs) Run() (string, error) {
	err := os.WriteFile(a.Filename, []byte(a.Content), 0644)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("File %s has been modified successfully.", a.Filename), nil
}

func (a *ModifyFileArgs) PrettyCommand() string {
	return fmt.Sprintf("cat > %s <<-EOF\n%s\n\nEOF\n# destination: %s", a.Filename, a.Content, a.Filename)
}

type AppendToFileArgs struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

func (a *AppendToFileArgs) Run() (string, error) {
	f, err := os.OpenFile(a.Filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = f.Write([]byte(a.Content))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("File %s has been modified successfully.", a.Filename), nil
}

func (a *AppendToFileArgs) PrettyCommand() string {
	return fmt.Sprintf("cat >> %s <<-EOF\n%s\n\nEOF\n# destination: %s", a.Filename, a.Content, a.Filename)
}

type ReplaceStringInFileArgs struct {
	Filename       string
	OriginalString string
	NewString      string
	Count          int
}

func (a *ReplaceStringInFileArgs) Run() (string, error) {
	content, err := os.ReadFile(a.Filename)
	if err != nil {
		return "", err
	}

	actualCount, rep := replaceStringCount(string(content), a.OriginalString, a.NewString, a.Count)
	err = os.WriteFile(a.Filename, []byte(rep), 0644)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Replaced string in file %s %d times.", a.Filename, actualCount), nil
}

func (a *ReplaceStringInFileArgs) PrettyCommand() string {
	return fmt.Sprintf("# replace string in file %s (count %d)\n==== old ====\n%s\n==== new ====%s\n====     ====\n# in %s", a.Filename, a.Count, a.OriginalString, a.NewString, a.Filename)
}

func replaceStringCount(s, old, new string, n int) (int, string) {
	if old == new || n == 0 {
		return 0, s // avoid allocation
	}

	// Compute number of replacements.
	if m := strings.Count(s, old); m == 0 {
		return 0, s // avoid allocation
	} else if n < 0 || m < n {
		n = m
	}

	// Apply replacements to buffer.
	var replacementCount int
	var b strings.Builder
	b.Grow(len(s) + n*(len(new)-len(old)))
	start := 0
	for i := 0; i < n; i++ {
		j := start
		if len(old) == 0 {
			if i > 0 {
				_, wid := utf8.DecodeRuneInString(s[start:])
				j += wid
			}
		} else {
			j += strings.Index(s[start:], old)
		}
		b.WriteString(s[start:j])
		b.WriteString(new)
		start = j + len(old)
		replacementCount++
	}
	b.WriteString(s[start:])
	return replacementCount, b.String()
}
