package site

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"git.zexi.me/zexi/atrace/server/auth"
)

//go:embed archive-help.md
var archiveReadme string

//go:embed terminal.html
var terminalPage string

const shellOutputLimit = 256 << 10

var errShellOutput = errors.New("结果太长，请缩小路径范围或用 head 限制行数")

type shellResult struct {
	CWD    string `json:"cwd"`
	Output string `json:"stdout"`
	Error  string `json:"stderr"`
	Status int    `json:"status"`
}

type shellToken struct {
	text     string
	operator bool
}

// shellWords understands literal arguments, quotes and the operators handled below.
func shellWords(command string) ([]shellToken, error) {
	var tokens []shellToken
	var word strings.Builder
	var quote rune
	active, escaped := false, false
	flush := func() {
		if active {
			tokens = append(tokens, shellToken{text: word.String()})
			word.Reset()
			active = false
		}
	}
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case escaped:
			word.WriteRune(c)
			escaped = false
		case c == '\\' && quote != '\'':
			escaped, active = true, true
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				word.WriteRune(c)
			}
		case c == '\'' || c == '"':
			quote, active = c, true
		case unicode.IsSpace(c):
			flush()
		case c == '|' || c == ';' || c == '&':
			flush()
			op := string(c)
			if c == '&' {
				if i+1 == len(runes) || runes[i+1] != '&' {
					return nil, errors.New("支持的连接符是 |、&& 和 ;")
				}
				i++
				op = "&&"
			}
			tokens = append(tokens, shellToken{text: op, operator: true})
		case strings.ContainsRune("<>`", c):
			return nil, errors.New("命令只支持字面参数和管道")
		default:
			active = true
			word.WriteRune(c)
		}
	}
	if escaped || quote != 0 {
		return nil, errors.New("引号或转义未闭合")
	}
	flush()
	return tokens, nil
}

func (a *archive) execute(cwd, command string) shellResult {
	result := shellResult{CWD: cwd}
	tokens, err := shellWords(command)
	if err != nil {
		result.Error, result.Status = err.Error(), 2
		return result
	}
	// Parse the whole line before applying a command such as cd.
	var stages [][]string
	var operators []string
	args := []string{}
	for _, token := range tokens {
		if !token.operator {
			args = append(args, token.text)
			continue
		}
		if len(args) == 0 {
			result.Error, result.Status = "连接符前缺少命令", 2
			return result
		}
		stages = append(stages, args)
		operators = append(operators, token.text)
		args = nil
	}
	if len(args) != 0 {
		stages = append(stages, args)
	} else if len(operators) != 0 {
		if operators[len(operators)-1] != ";" {
			result.Error, result.Status = "连接符后缺少命令", 2
			return result
		}
		operators = operators[:len(operators)-1]
	}
	for start := 0; start < len(stages); {
		end := start
		for end < len(operators) && operators[end] == "|" {
			end++
		}
		if start == 0 || operators[start-1] != "&&" || result.Status == 0 {
			var input *string
			var output string
			for i := start; i <= end; i++ {
				if i < end {
					// A piped head can limit how much the producer keeps.
					if limit, ok := headLines(stages[i+1]); ok && stages[i][0] == "grep" {
						stages[i] = append([]string{"grep", "-m", strconv.Itoa(limit)}, stages[i][1:]...)
					}
				}
				localCWD := &result.CWD
				if start != end {
					tmp := result.CWD
					localCWD = &tmp
				}
				output, err = a.run(localCWD, stages[i], input)
				input = &output
				if err != nil {
					break
				}
			}
			result.Status = 0
			if err != nil {
				if errors.Is(err, errNoMatch) {
					result.Status = 1
				} else {
					result.Error += shellError(err) + "\n"
					result.Status = 2
				}
			}
			if len(result.Output)+len(output) > shellOutputLimit {
				result.Error += errShellOutput.Error() + "\n"
				result.Status = 2
				return result
			}
			result.Output += output
		}
		start = end + 1
	}
	return result
}

var errNoMatch = errors.New("no match")

func shellError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "查询超时，请缩小到指定人物或日期"
	case errors.Is(err, fs.ErrNotExist):
		return "路径不存在"
	}
	return err.Error()
}

func (a *archive) run(cwd *string, args []string, input *string) (string, error) {
	if err := a.ctx.Err(); err != nil {
		return "", err
	}
	var out strings.Builder
	write := func(text string) error {
		if len(text)+out.Len() > shellOutputLimit {
			return errShellOutput
		}
		out.WriteString(text)
		return nil
	}
	target := func(args []string) (string, error) {
		if len(args) == 0 {
			return *cwd, nil
		}
		if len(args) != 1 {
			return "", errors.New("请指定一个路径")
		}
		return archivePath(*cwd, args[0])
	}
	switch args[0] {
	case "help":
		return "pwd · cd · ls · find [-name 模式] · cat · grep [-rinl] [-m 行数] · head [-n 行数] · wc -l\n连接符 | && ;\n", nil
	case "pwd":
		return *cwd + "\n", nil
	case "cd":
		name := archiveRoot
		if len(args) > 1 {
			var err error
			name, err = target(args[1:])
			if err != nil {
				return "", err
			}
		}
		if _, err := a.list(name); err != nil {
			return "", err
		}
		*cwd = name
	case "ls":
		name, err := target(args[1:])
		if err != nil {
			return "", err
		}
		children, err := a.list(name)
		if errors.Is(err, fs.ErrNotExist) && a.fileExists(name) == nil {
			return path.Base(name) + "\n", nil
		}
		if err != nil {
			return "", err
		}
		for _, child := range children {
			if err := write(child + "\n"); err != nil {
				return out.String(), err
			}
		}
	case "cat":
		if len(args) == 1 && input != nil {
			return *input, nil
		}
		if len(args) < 2 {
			return "", errors.New("用法：cat 文件...")
		}
		for _, arg := range args[1:] {
			name, err := archivePath(*cwd, arg)
			if err != nil {
				return out.String(), err
			}
			body, text, err := a.read(name)
			if err != nil {
				return out.String(), err
			}
			if !text {
				body.Close()
				return out.String(), errors.New("媒体文件可通过文件下载接口读取")
			}
			data, err := io.ReadAll(io.LimitReader(body, shellOutputLimit+1))
			body.Close()
			if err != nil {
				return out.String(), err
			}
			if err := write(string(data)); err != nil {
				return out.String(), err
			}
		}
	case "find":
		fields := args[1:]
		var pattern string
		if len(fields) >= 2 && fields[len(fields)-2] == "-name" {
			pattern = fields[len(fields)-1]
			fields = fields[:len(fields)-2]
			if _, err := path.Match(pattern, ""); err != nil {
				return "", err
			}
		}
		name, err := target(fields)
		if err != nil {
			return "", err
		}
		err = a.walk(name, func(name string, _ bool) error {
			if pattern != "" {
				if match, _ := path.Match(pattern, path.Base(name)); !match {
					return nil
				}
			}
			return write(name + "\n")
		})
		return out.String(), err
	case "grep":
		return a.grep(*cwd, args[1:], input, write, &out)
	case "head":
		lines, ok := headLines(args)
		if !ok || input == nil {
			return "", errors.New("用法：命令 | head [-n 行数]")
		}
		value := *input
		for i := 0; i < len(value); i++ {
			if value[i] == '\n' {
				lines--
				if lines == 0 {
					return value[:i+1], nil
				}
			}
		}
		return value, nil
	case "wc":
		if len(args) != 2 || args[1] != "-l" || input == nil {
			return "", errors.New("用法：命令 | wc -l")
		}
		return strconv.Itoa(strings.Count(*input, "\n")) + "\n", nil
	default:
		return "", fmt.Errorf("未知命令 %q，输入 help 查看可用命令", args[0])
	}
	return out.String(), nil
}

func headLines(args []string) (int, bool) {
	if args[0] != "head" {
		return 0, false
	}
	if len(args) == 1 {
		return 10, true
	}
	if len(args) == 3 && args[1] == "-n" {
		n, err := strconv.Atoi(args[2])
		return n, err == nil && n > 0
	}
	return 0, false
}

var errMatchLimit = errors.New("match limit reached")

func (a *archive) grep(cwd string, args []string, input *string, write func(string) error, out *strings.Builder) (string, error) {
	recursive, ignoreCase, numbers, names := false, false, false, false
	limit := 0
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		flag := args[0]
		args = args[1:]
		if flag == "--" {
			break
		}
		if flag == "-m" && len(args) > 0 {
			n, err := strconv.Atoi(args[0])
			if err != nil || n < 1 {
				return "", errors.New("-m 需要正整数")
			}
			if limit == 0 || n < limit {
				limit = n
			}
			args = args[1:]
			continue
		}
		for _, char := range strings.TrimPrefix(flag, "-") {
			switch char {
			case 'r', 'R':
				recursive = true
			case 'i':
				ignoreCase = true
			case 'n':
				numbers = true
			case 'l':
				names = true
			case 'F':
			default:
				return "", fmt.Errorf("不支持的 grep 参数 %s", flag)
			}
		}
	}
	if len(args) == 0 || args[0] == "" {
		return "", errors.New("用法：grep [-rinl] 文本 [路径...]")
	}
	keyword, paths := args[0], args[1:]
	if ignoreCase {
		keyword = strings.ToLower(keyword)
	}
	matches := 0
	scan := func(name string, reader io.Reader) error {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), 2<<20)
		for n := 1; scanner.Scan(); n++ {
			if err := a.ctx.Err(); err != nil {
				return err
			}
			line := scanner.Text()
			value := line
			if ignoreCase {
				value = strings.ToLower(value)
			}
			if !strings.Contains(value, keyword) {
				continue
			}
			matches++
			prefix := ""
			if name != "" {
				prefix = name + ":"
			}
			if numbers {
				prefix += strconv.Itoa(n) + ":"
			}
			if names && name != "" {
				if err := write(name + "\n"); err != nil {
					return err
				}
			} else if err := write(prefix + line + "\n"); err != nil {
				return err
			}
			if limit > 0 && matches >= limit {
				return errMatchLimit
			}
			if names {
				break
			}
		}
		return scanner.Err()
	}
	read := func(name string, directory bool) error {
		if directory {
			return nil
		}
		reader, text, err := a.read(name)
		if err != nil {
			return err
		}
		defer reader.Close()
		if !text {
			return nil
		}
		return scan(name, reader)
	}
	var err error
	if input != nil && len(paths) == 0 {
		err = scan("", strings.NewReader(*input))
	} else {
		if len(paths) == 0 {
			paths = []string{"."}
		}
		for _, name := range paths {
			var full string
			full, err = archivePath(cwd, name)
			if err != nil {
				break
			}
			if recursive {
				err = a.walk(full, read)
			} else {
				err = read(full, false)
			}
			if err != nil {
				break
			}
		}
	}
	if err == nil && matches == 0 {
		err = errNoMatch
	}
	if errors.Is(err, errMatchLimit) {
		err = nil
	}
	return out.String(), err
}

func (renderer *Renderer) shell(w http.ResponseWriter, r *http.Request) {
	select {
	case renderer.shellSlots <- struct{}{}:
		defer func() { <-renderer.shellSlots }()
	default:
		http.Error(w, "终端繁忙，请稍后重试", http.StatusTooManyRequests)
		return
	}
	var request struct{ Command, CWD string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&request); err != nil ||
		len(request.Command) > 8192 || !utf8.ValidString(request.Command) {
		http.Error(w, "命令请求无效", http.StatusBadRequest)
		return
	}
	cwd := archiveRoot
	if request.CWD != "" {
		var err error
		cwd, err = archivePath(archiveRoot, request.CWD)
		if err != nil {
			http.Error(w, "工作目录无效", http.StatusBadRequest)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	a := archive{ctx: ctx, renderer: renderer, owner: auth.Owner(ctx)}
	if request.CWD != "" && cwd != archiveRoot {
		if _, err := a.list(cwd); err != nil {
			cwd = archiveRoot
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(a.execute(cwd, request.Command)); err != nil {
		log.Printf("shell: %v", err)
	}
}

func (renderer *Renderer) archiveDownload(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	a := archive{ctx: ctx, renderer: renderer, owner: auth.Owner(ctx)}
	name, err := archivePath(archiveRoot, r.URL.Query().Get("path"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	reader, text, err := a.read(name)
	if errors.Is(err, fs.ErrNotExist) && a.owner {
		// A shared visitor URL keeps the same view when opened by the owner.
		visitor := archive{ctx: auth.Visitor(ctx), renderer: renderer}
		reader, text, err = visitor.read(name)
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer reader.Close()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Type", "application/octet-stream")
	if text {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(name)}))
	_, _ = io.Copy(w, reader)
}
