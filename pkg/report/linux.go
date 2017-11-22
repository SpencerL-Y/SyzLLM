// Copyright 2017 syzkaller project authors. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

package report

import (
	"bufio"
	"bytes"
	"fmt"
	"net/mail"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/syzkaller/pkg/osutil"
	"github.com/google/syzkaller/pkg/symbolizer"
)

type linux struct {
	kernelSrc           string
	kernelObj           string
	vmlinux             string
	symbols             map[string][]symbolizer.Symbol
	ignores             []*regexp.Regexp
	consoleOutputRe     *regexp.Regexp
	questionableRe      *regexp.Regexp
	guiltyFileBlacklist []*regexp.Regexp
	eoi                 []byte
}

func ctorLinux(kernelSrc, kernelObj string, symbols map[string][]symbolizer.Symbol,
	ignores []*regexp.Regexp) (Reporter, error) {
	vmlinux := filepath.Join(kernelObj, "vmlinux")
	if symbols == nil {
		var err error
		symbols, err = symbolizer.ReadSymbols(vmlinux)
		if err != nil {
			return nil, err
		}
	}
	ctx := &linux{
		kernelSrc: kernelSrc,
		kernelObj: kernelObj,
		vmlinux:   vmlinux,
		symbols:   symbols,
		ignores:   ignores,
	}
	ctx.consoleOutputRe = regexp.MustCompile(`^(?:\*\* [0-9]+ printk messages dropped \*\* )?(?:\<[0-9]+\>)?\[ *[0-9]+\.[0-9]+\] `)
	ctx.questionableRe = regexp.MustCompile(`(?:\[\<[0-9a-f]+\>\])? \? +[a-zA-Z0-9_.]+\+0x[0-9a-f]+/[0-9a-f]+`)
	ctx.eoi = []byte("<EOI>")
	ctx.guiltyFileBlacklist = []*regexp.Regexp{
		regexp.MustCompile(`.*\.h`),
		regexp.MustCompile(`^lib/.*`),
		regexp.MustCompile(`^virt/lib/.*`),
		regexp.MustCompile(`^mm/kasan/.*`),
		regexp.MustCompile(`^mm/kmsan/.*`),
		regexp.MustCompile(`^mm/percpu.*`),
		regexp.MustCompile(`^mm/vmalloc.c`),
		regexp.MustCompile(`^mm/page_alloc.c`),
		regexp.MustCompile(`^kernel/rcu/.*`),
		regexp.MustCompile(`^arch/.*/kernel/traps.c`),
		regexp.MustCompile(`^kernel/locking/*`),
		regexp.MustCompile(`^kernel/panic.c`),
		regexp.MustCompile(`^kernel/softirq.c`),
		regexp.MustCompile(`^net/core/dev.c`),
		regexp.MustCompile(`^net/core/sock.c`),
		regexp.MustCompile(`^net/core/skbuff.c`),
	}
	return ctx, nil
}

func (ctx *linux) ContainsCrash(output []byte) bool {
	return containsCrash(output, linuxOopses, ctx.ignores)
}

func (ctx *linux) Parse(output []byte) *Report {
	rep := new(Report)
	var oops *oops
	var textPrefix [][]byte
	textLines := 0
	skipText := false
	for pos := 0; pos < len(output); {
		next := bytes.IndexByte(output[pos:], '\n')
		if next != -1 {
			next += pos
		} else {
			next = len(output)
		}
		for _, oops1 := range linuxOopses {
			match := matchOops(output[pos:next], oops1, ctx.ignores)
			if match == -1 {
				continue
			}
			if oops == nil {
				oops = oops1
				rep.StartPos = pos
				rep.Title = string(output[pos+match : next])
			}
			rep.EndPos = next
		}
		if ctx.consoleOutputRe.Match(output[pos:next]) &&
			(!ctx.questionableRe.Match(output[pos:next]) ||
				bytes.Index(output[pos:next], ctx.eoi) != -1) {
			lineStart := bytes.Index(output[pos:next], []byte("] ")) + pos + 2
			lineEnd := next
			if lineEnd != 0 && output[lineEnd-1] == '\r' {
				lineEnd--
			}
			if oops == nil {
				textPrefix = append(textPrefix, append([]byte{}, output[lineStart:lineEnd]...))
				if len(textPrefix) > 5 {
					textPrefix = textPrefix[1:]
				}
			} else {
				// Prepend 5 lines preceding start of the report,
				// they can contain additional info related to the report.
				for _, prefix := range textPrefix {
					rep.Report = append(rep.Report, prefix...)
					rep.Report = append(rep.Report, '\n')
				}
				textPrefix = nil
				textLines++
				ln := output[lineStart:lineEnd]
				skipLine := skipText
				if bytes.Contains(ln, []byte("Disabling lock debugging due to kernel taint")) {
					skipLine = true
				} else if textLines > 40 && bytes.Contains(ln, []byte("Kernel panic - not syncing")) {
					// If panic_on_warn set, then we frequently have 2 stacks:
					// one for the actual report (or maybe even more than one),
					// and then one for panic caused by panic_on_warn. This makes
					// reports unnecessary long and the panic (current) stack
					// is always present in the actual report. So we strip the
					// panic message. However, we check that we have enough lines
					// before the panic, because sometimes we have, for example,
					// a single WARNING line without a stack and then the panic
					// with the stack.
					skipText = true
					skipLine = true
				}
				if !skipLine {
					rep.Report = append(rep.Report, ln...)
					rep.Report = append(rep.Report, '\n')
				}
			}
		}
		pos = next + 1
	}
	if oops == nil {
		return nil
	}
	rep.Title, rep.Corrupted = extractDescription(output[rep.StartPos:], oops)
	if !rep.Corrupted {
		rep.Corrupted = ctx.isCorrupted(rep.Title, rep.Report)
	}
	// Executor PIDs are not interesting.
	rep.Title = executorRe.ReplaceAllLiteralString(rep.Title, "syz-executor")
	// Replace that everything looks like an address with "ADDR",
	// addresses in descriptions can't be good regardless of the oops regexps.
	rep.Title = addrRe.ReplaceAllLiteralString(rep.Title, "ADDR")
	// Replace that everything looks like a decimal number with "NUM".
	rep.Title = decNumRe.ReplaceAllLiteralString(rep.Title, "NUM")
	// Replace that everything looks like a file line number with "LINE".
	rep.Title = lineNumRe.ReplaceAllLiteralString(rep.Title, ":LINE")
	// Replace all raw references to runctions (e.g. "ip6_fragment+0x1052/0x2d80")
	// with just function name ("ip6_fragment"). Offsets and sizes are not stable.
	rep.Title = funcRe.ReplaceAllString(rep.Title, "$1")
	// CPU numbers are not interesting.
	rep.Title = cpuRe.ReplaceAllLiteralString(rep.Title, "CPU")
	return rep
}

func (ctx *linux) Symbolize(text []byte) ([]byte, error) {
	symb := symbolizer.NewSymbolizer()
	defer symb.Close()
	// Strip vmlinux location from all paths.
	strip, _ := filepath.Abs(ctx.vmlinux)
	strip = filepath.Dir(strip) + string(filepath.Separator)
	// Vmlinux may have been moved, so check if we can find debug info
	// for __sanitizer_cov_trace_pc. We know where it is located,
	// so we can infer correct strip prefix from it.
	if covSymbols := ctx.symbols["__sanitizer_cov_trace_pc"]; len(covSymbols) != 0 {
		for _, covSymb := range covSymbols {
			frames, _ := symb.Symbolize(ctx.vmlinux, covSymb.Addr)
			if len(frames) > 0 {
				file := frames[len(frames)-1].File
				if idx := strings.Index(file, "kernel/kcov.c"); idx != -1 {
					strip = file[:idx]
					break
				}
			}
		}
	}
	var symbolized []byte
	s := bufio.NewScanner(bytes.NewReader(text))
	for s.Scan() {
		line := append([]byte{}, s.Bytes()...)
		line = append(line, '\n')
		line = symbolizeLine(symb.Symbolize, ctx.symbols, ctx.vmlinux, strip, line)
		symbolized = append(symbolized, line...)
	}
	return symbolized, nil
}

func symbolizeLine(symbFunc func(bin string, pc uint64) ([]symbolizer.Frame, error),
	symbols map[string][]symbolizer.Symbol, vmlinux, strip string, line []byte) []byte {
	match := linuxSymbolizeRe.FindSubmatchIndex(line)
	if match == nil {
		return line
	}
	fn := line[match[2]:match[3]]
	off, err := strconv.ParseUint(string(line[match[4]:match[5]]), 16, 64)
	if err != nil {
		return line
	}
	size, err := strconv.ParseUint(string(line[match[6]:match[7]]), 16, 64)
	if err != nil {
		return line
	}
	symb := symbols[string(fn)]
	if len(symb) == 0 {
		return line
	}
	var funcStart uint64
	for _, s := range symb {
		if funcStart == 0 || int(size) == s.Size {
			funcStart = s.Addr
		}
	}
	frames, err := symbFunc(vmlinux, funcStart+off-1)
	if err != nil || len(frames) == 0 {
		return line
	}
	var symbolized []byte
	for _, frame := range frames {
		file := frame.File
		if strings.HasPrefix(file, strip) {
			file = file[len(strip):]
		}
		if strings.HasPrefix(file, "./") {
			file = file[2:]
		}
		info := fmt.Sprintf(" %v:%v", file, frame.Line)
		modified := append([]byte{}, line...)
		modified = replace(modified, match[7], match[7], []byte(info))
		if frame.Inline {
			end := match[7] + len(info)
			modified = replace(modified, end, end, []byte(" [inline]"))
			modified = replace(modified, match[2], match[7], []byte(frame.Func))
		}
		symbolized = append(symbolized, modified...)
	}
	return symbolized
}

func (ctx *linux) ExtractConsoleOutput(output []byte) (result []byte) {
	for pos := 0; pos < len(output); {
		next := bytes.IndexByte(output[pos:], '\n')
		if next != -1 {
			next += pos
		} else {
			next = len(output)
		}
		if ctx.consoleOutputRe.Match(output[pos:next]) &&
			(!ctx.questionableRe.Match(output[pos:next]) ||
				bytes.Index(output[pos:next], ctx.eoi) != -1) {
			lineStart := bytes.Index(output[pos:next], []byte("] ")) + pos + 2
			lineEnd := next
			if lineEnd != 0 && output[lineEnd-1] == '\r' {
				lineEnd--
			}
			result = append(result, output[lineStart:lineEnd]...)
			result = append(result, '\n')
		}
		pos = next + 1
	}
	return
}

func (ctx *linux) ExtractGuiltyFile(report []byte) string {
	files := ctx.extractFiles(report)
nextFile:
	for _, file := range files {
		for _, re := range ctx.guiltyFileBlacklist {
			if re.MatchString(file) {
				continue nextFile
			}
		}
		return file
	}
	return ""
}

func (ctx *linux) GetMaintainers(file string) ([]string, error) {
	mtrs, err := ctx.getMaintainers(file, false)
	if err != nil {
		return nil, err
	}
	if len(mtrs) <= 1 {
		mtrs, err = ctx.getMaintainers(file, true)
		if err != nil {
			return nil, err
		}
	}
	return mtrs, nil
}

func (ctx *linux) getMaintainers(file string, blame bool) ([]string, error) {
	args := []string{"--no-n", "--no-rolestats"}
	if blame {
		args = append(args, "--git-blame")
	}
	args = append(args, file)
	output, err := osutil.RunCmd(time.Minute, ctx.kernelSrc, filepath.FromSlash("scripts/get_maintainer.pl"), args...)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(output), "\n")
	var mtrs []string
	for _, line := range lines {
		addr, err := mail.ParseAddress(line)
		if err != nil {
			continue
		}
		mtrs = append(mtrs, addr.Address)
	}
	return mtrs, nil
}

func (ctx *linux) extractFiles(report []byte) []string {
	matches := filenameRe.FindAll(report, -1)
	var files []string
	for _, match := range matches {
		files = append(files, string(bytes.Split(match, []byte{':'})[0]))
	}
	return files
}

func (ctx *linux) isCorrupted(title string, report []byte) bool {
	// Report must contain 'Call Trace' or 'backtrace'.
	if !bytes.Contains(report, []byte("Call Trace")) && !bytes.Contains(report, []byte("backtrace")) {
		return true
	}
	// Check for common title corruptions.
	for _, re := range linuxCorruptedTitles {
		if re.MatchString(title) {
			return true
		}
	}
	// When a report contains 'Call Trace', 'backtrace', 'Allocated' or 'Freed' keywords,
	// it must also contain at least a single stack frame after each them.
	for _, key := range linuxStackKeywords {
		match := key.FindSubmatchIndex(report)
		if match == nil {
			continue
		}
		parts := bytes.Split(report[match[0]:], []byte{'\n'})
		if len(parts) < 2 {
			return true
		}
		frame := parts[1]
		if bytes.Equal(bytes.TrimSpace(frame), []byte("<IRQ>")) {
			if len(parts) < 3 {
				return true
			}
			frame = parts[2]
		}
		if !linuxSymbolizeRe.Match(frame) {
			return true
		}
	}
	return false
}

var (
	filenameRe       = regexp.MustCompile(`[a-zA-Z0-9_\-\./]*[a-zA-Z0-9_\-]+\.(c|h):[0-9]+`)
	linuxSymbolizeRe = regexp.MustCompile(`(?:\[\<(?:[0-9a-f]+)\>\])?[ \t]+(?:[0-9]+:)?([a-zA-Z0-9_.]+)\+0x([0-9a-f]+)/0x([0-9a-f]+)`)
	decNumRe         = regexp.MustCompile(`[0-9]{5,}`)
	lineNumRe        = regexp.MustCompile(`(:[0-9]+)+`)
	addrRe           = regexp.MustCompile(`[0-9a-f]{8,}`)
	funcRe           = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9_.]+)\+0x[0-9a-z]+/0x[0-9a-z]+`)
	cpuRe            = regexp.MustCompile(`CPU#[0-9]+`)
	executorRe       = regexp.MustCompile(`syz-executor[0-9]+((/|:)[0-9]+)?`)
)

var linuxCorruptedTitles = []*regexp.Regexp{
	// Sometimes timestamps get merged into the middle of report description.
	regexp.MustCompile(`\[ *[0-9]+\.[0-9]+\]`),
}

var linuxStackKeywords = []*regexp.Regexp{
	regexp.MustCompile(`Call Trace`),
	regexp.MustCompile(`Allocated`),
	regexp.MustCompile(`Freed`),
	// Match 'backtrace:', but exclude 'stack backtrace:'
	regexp.MustCompile(`[^k] backtrace:`),
}

var linuxOopses = []*oops{
	&oops{
		[]byte("BUG:"),
		[]oopsFormat{
			{
				compile("BUG: KASAN: ([a-z\\-]+) in {{FUNC}}(?:.*\\n)+?.*(Read|Write) of size ([0-9]+)"),
				compile("BUG: KASAN: (?:.*\\n)+?.*Call Trace:"),
				"KASAN: %[1]v %[3]v in %[2]v",
				false,
			},
			{
				compile("BUG: KASAN: ([a-z\\-]+) on address(?:.*\\n)+?.*(Read|Write) of size ([0-9]+)"),
				compile("BUG: KASAN: (?:.*\\n)+?.*Call Trace:"),
				"KASAN: %[1]v %[2]v",
				false,
			},
			{
				compile("BUG: KASAN: (.*)"),
				compile("BUG: KASAN: (?:.*\\n)+?.*Call Trace:"),
				"KASAN: %[1]v",
				false,
			},
			{
				compile("BUG: unable to handle kernel paging request(?:.*\\n)+?.*IP: (?:{{PC}} +)?{{FUNC}}"),
				nil,
				"BUG: unable to handle kernel paging request in %[1]v",
				false,
			},
			{
				compile("BUG: unable to handle kernel paging request"),
				nil,
				"BUG: unable to handle kernel paging request",
				false,
			},
			{
				compile("BUG: unable to handle kernel NULL pointer dereference(?:.*\\n)+?.*IP: (?:{{PC}} +)?{{FUNC}}"),
				nil,
				"BUG: unable to handle kernel NULL pointer dereference in %[1]v",
				false,
			},
			{
				// Sometimes with such BUG failures, the second part of the header doesn't get printed
				// or gets corrupted, because kernel prints it as two separate printk() calls.
				compile("BUG: unable to handle kernel"),
				nil,
				"BUG: unable to handle kernel",
				true,
			},
			{
				compile("BUG: spinlock (lockup suspected|already unlocked|recursion|bad magic|wrong owner|wrong CPU)"),
				nil,
				"BUG: spinlock %[1]v",
				false,
			},
			{
				compile("BUG: soft lockup"),
				nil,
				"BUG: soft lockup",
				false,
			},
			{
				compile("BUG: .*still has locks held!(?:.*\\n)+?.*{{PC}} +{{FUNC}}"),
				nil,
				"BUG: still has locks held in %[1]v",
				false,
			},
			{
				compile("BUG: bad unlock balance detected!(?:.*\\n)+?.*{{PC}} +{{FUNC}}"),
				nil,
				"BUG: bad unlock balance in %[1]v",
				false,
			},
			{
				// If we failed to extract function name where the fault happened,
				// the report is most likely truncated.
				compile("BUG: bad unlock balance detected!"),
				nil,
				"BUG: bad unlock balance",
				true,
			},
			{
				compile("BUG: held lock freed!(?:.*\\n)+?.*{{PC}} +{{FUNC}}"),
				nil,
				"BUG: held lock freed in %[1]v",
				false,
			},
			{
				compile("BUG: Bad rss-counter state"),
				nil,
				"BUG: Bad rss-counter state",
				false,
			},
			{
				compile("BUG: non-zero nr_ptes on freeing mm"),
				nil,
				"BUG: non-zero nr_ptes on freeing mm",
				false,
			},
			{
				compile("BUG: non-zero nr_pmds on freeing mm"),
				nil,
				"BUG: non-zero nr_pmds on freeing mm",
				false,
			},
			{
				compile("BUG: Dentry .* still in use \\([0-9]+\\) \\[unmount of ([^\\]]+)\\]"),
				nil,
				"BUG: Dentry still in use [unmount of %[1]v]",
				false,
			},
			{
				compile("BUG: Bad page state.*"),
				nil,
				"BUG: Bad page state",
				false,
			},
			{
				compile("BUG: spinlock bad magic.*"),
				nil,
				"BUG: spinlock bad magic",
				false,
			},
			{
				compile("BUG: workqueue lockup.*"),
				nil,
				"BUG: workqueue lockup",
				false,
			},
		},
		[]*regexp.Regexp{
			// Android prints this sometimes during boot.
			compile("Boot_DEBUG:"),
		},
	},
	&oops{
		[]byte("WARNING:"),
		[]oopsFormat{
			{
				compile("WARNING: .* at {{SRC}} {{FUNC}}"),
				nil,
				"WARNING in %[2]v",
				false,
			},
			{
				compile("WARNING: possible circular locking dependency detected(?:.*\\n)+?.*is trying to acquire lock(?:.*\\n)+?.*at: {{PC}} +{{FUNC}}"),
				nil,
				"possible deadlock in %[1]v",
				false,
			},
			{
				compile("WARNING: possible circular locking dependency detected"),
				nil,
				"possible deadlock",
				false,
			},
			{
				compile("WARNING: possible irq lock inversion dependency detected(?:.*\\n)+?.*just changed the state of lock(?:.*\\n)+?.*at: {{PC}} +{{FUNC}}"),
				nil,
				"possible deadlock in %[1]v",
				false,
			},
			{
				compile("WARNING: possible irq lock inversion dependency detected"),
				nil,
				"possible deadlock",
				false,
			},
			{
				compile("WARNING: SOFTIRQ-safe -> SOFTIRQ-unsafe lock order detected(?:.*\\n)+?.*is trying to acquire(?:.*\\n)+?.*at: {{PC}} +{{FUNC}}"),
				nil,
				"possible deadlock in %[1]v",
				false,
			},
			{
				compile("WARNING: SOFTIRQ-safe -> SOFTIRQ-unsafe lock order detected"),
				nil,
				"possible deadlock",
				false,
			},
			{
				compile("WARNING: possible recursive locking detected(?:.*\\n)+?.*is trying to acquire lock(?:.*\\n)+?.*at: {{PC}} +{{FUNC}}"),
				nil,
				"possible deadlock in %[1]v",
				false,
			},
			{
				compile("WARNING: possible recursive locking detected"),
				nil,
				"possible deadlock",
				false,
			},
			{
				compile("WARNING: inconsistent lock state(?:.*\\n)+?.*takes(?:.*\\n)+?.*at: {{PC}} +{{FUNC}}"),
				nil,
				"inconsistent lock state in %[1]v",
				false,
			},
			{
				compile("WARNING: suspicious RCU usage(?:.*\n)+?.*?{{SRC}}"),
				nil,
				"suspicious RCU usage at %[1]v",
				false,
			},
			{
				compile("WARNING: kernel stack regs at [0-9a-f]+ in [^ ]* has bad '([^']+)' value"),
				nil,
				"WARNING: kernel stack regs has bad '%[1]v' value",
				false,
			},
			{
				compile("WARNING: kernel stack frame pointer at [0-9a-f]+ in [^ ]* has bad value"),
				nil,
				"WARNING: kernel stack frame pointer has bad value",
				false,
			},
		},
		[]*regexp.Regexp{
			compile("WARNING: /etc/ssh/moduli does not exist, using fixed modulus"), // printed by sshd
		},
	},
	&oops{
		[]byte("INFO:"),
		[]oopsFormat{
			{
				compile("INFO: possible circular locking dependency detected \\](?:.*\\n)+?.*is trying to acquire lock(?:.*\\n)+?.*at: {{PC}} +{{FUNC}}"),
				nil,
				"possible deadlock in %[1]v",
				false,
			},
			{
				compile("INFO: possible circular locking dependency detected"),
				nil,
				"possible deadlock",
				false,
			},
			{
				compile("INFO: possible irq lock inversion dependency detected \\](?:.*\\n)+?.*just changed the state of lock(?:.*\\n)+?.*at: {{PC}} +{{FUNC}}"),
				nil,
				"possible deadlock in %[1]v",
				false,
			},
			{
				compile("INFO: possible irq lock inversion dependency detected"),
				nil,
				"possible deadlock",
				false,
			},
			{
				compile("INFO: SOFTIRQ-safe -> SOFTIRQ-unsafe lock order detected \\](?:.*\\n)+?.*is trying to acquire(?:.*\\n)+?.*at: {{PC}} +{{FUNC}}"),
				nil,
				"possible deadlock in %[1]v",
				false,
			},
			{
				compile("INFO: SOFTIRQ-safe -> SOFTIRQ-unsafe lock order detected"),
				nil,
				"possible deadlock",
				false,
			},
			{
				compile("INFO: possible recursive locking detected \\](?:.*\\n)+?.*is trying to acquire lock(?:.*\\n)+?.*at: {{PC}} +{{FUNC}}"),
				nil,
				"possible deadlock in %[1]v",
				false,
			},
			{
				compile("INFO: possible recursive locking detected"),
				nil,
				"possible deadlock",
				false,
			},
			{
				compile("INFO: inconsistent lock state \\](?:.*\\n)+?.*takes(?:.*\\n)+?.*at: {{PC}} +{{FUNC}}"),
				nil,
				"inconsistent lock state in %[1]v",
				false,
			},
			{
				compile("INFO: rcu_preempt detected stalls(?:.*\\n)+?.*</IRQ>.*\n(?:.* \\? .*\\n)+?(?:.*rcu.*\\n)+?.*\\]  {{FUNC}}"),
				nil,
				"INFO: rcu detected stall in %[1]v",
				false,
			},
			{
				compile("INFO: rcu_preempt detected stalls"),
				nil,
				"INFO: rcu detected stall",
				false,
			},
			{
				compile("INFO: rcu_sched detected(?: expedited)? stalls(?:.*\\n)+?.*</IRQ>.*\n(?:.* \\? .*\\n)+?(?:.*rcu.*\\n)+?.*\\]  {{FUNC}}"),
				nil,
				"INFO: rcu detected stall in %[1]v",
				false,
			},
			{
				compile("INFO: rcu_sched detected(?: expedited)? stalls"),
				nil,
				"INFO: rcu detected stall",
				false,
			},
			{
				compile("INFO: rcu_preempt self-detected stall on CPU(?:.*\\n)+?.*</IRQ>.*\n(?:.* \\? .*\\n)+?(?:.*rcu.*\\n)+?.*\\]  {{FUNC}}"),
				nil,
				"INFO: rcu detected stall in %[1]v",
				false,
			},
			{
				compile("INFO: rcu_preempt self-detected stall on CPU"),
				nil,
				"INFO: rcu detected stall",
				false,
			},
			{
				compile("INFO: rcu_sched self-detected stall on CPU(?:.*\\n)+?.*</IRQ>.*\n(?:.* \\? .*\\n)+?(?:.*rcu.*\\n)+?.*\\]  {{FUNC}}"),
				nil,
				"INFO: rcu detected stall in %[1]v",
				false,
			},
			{
				compile("INFO: rcu_sched self-detected stall on CPU"),
				nil,
				"INFO: rcu detected stall",
				false,
			},
			{
				compile("INFO: rcu_bh detected stalls on CPU"),
				nil,
				"INFO: rcu detected stall",
				false,
			},
			{
				compile("INFO: suspicious RCU usage(?:.*\n)+?.*?{{SRC}}"),
				nil,
				"suspicious RCU usage at %[1]v",
				false,
			},
			{
				compile("INFO: task .* blocked for more than [0-9]+ seconds"),
				nil,
				"INFO: task hung",
				false,
			},
		},
		[]*regexp.Regexp{
			compile("INFO: lockdep is turned off"),
			compile("INFO: Stall ended before state dump start"),
			compile("INFO: NMI handler .* took too long to run"),
			compile("_INFO::"), // Android can print this during boot.
		},
	},
	&oops{
		[]byte("Unable to handle kernel paging request"),
		[]oopsFormat{
			{
				compile("Unable to handle kernel paging request(?:.*\\n)+?.*PC is at {{FUNC}}"),
				nil,
				"unable to handle kernel paging request in %[1]v",
				false,
			},
		},
		[]*regexp.Regexp{},
	},
	&oops{
		[]byte("general protection fault:"),
		[]oopsFormat{
			{
				compile("general protection fault:(?:.*\\n)+?.*RIP: [0-9]+:{{PC}} +{{PC}} +{{FUNC}}"),
				nil,
				"general protection fault in %[1]v",
				false,
			},
			{
				compile("general protection fault:(?:.*\\n)+?.*RIP: [0-9]+:{{FUNC}}"),
				nil,
				"general protection fault in %[1]v",
				false,
			},
			{
				// If we failed to extract function name where the fault happened,
				// the report is most likely truncated.
				compile("general protection fault"),
				nil,
				"general protection fault",
				true,
			},
		},
		[]*regexp.Regexp{},
	},
	&oops{
		[]byte("Kernel panic"),
		[]oopsFormat{
			{
				compile("Kernel panic - not syncing: Attempted to kill init!"),
				nil,
				"kernel panic: Attempted to kill init!",
				false,
			},
			{
				compile("Kernel panic - not syncing: Couldn't open N_TTY ldisc for [^ ]+ --- error -[0-9]+"),
				nil,
				"kernel panic: Couldn't open N_TTY ldisc",
				false,
			},
			{
				// 'kernel panic: Fatal exception' is usually printed after BUG,
				// so if we captured it as a report description, that means the
				// report got truncated and we missed the actual BUG header.
				compile("Kernel panic - not syncing: Fatal exception"),
				nil,
				"kernel panic: Fatal exception",
				true,
			},
			{
				// Same, but for WARNINGs and KASAN reports.
				compile("Kernel panic - not syncing: panic_on_warn set"),
				nil,
				"kernel panic: panic_on_warn set",
				true,
			},
			{
				compile("Kernel panic - not syncing: (.*)"),
				nil,
				"kernel panic: %[1]v",
				false,
			},
		},
		[]*regexp.Regexp{},
	},
	&oops{
		[]byte("kernel BUG"),
		[]oopsFormat{
			{
				compile("kernel BUG (.*)"),
				nil,
				"kernel BUG %[1]v",
				false,
			},
		},
		[]*regexp.Regexp{},
	},
	&oops{
		[]byte("Kernel BUG"),
		[]oopsFormat{
			{
				compile("Kernel BUG (.*)"),
				nil,
				"kernel BUG %[1]v",
				false,
			},
		},
		[]*regexp.Regexp{},
	},
	&oops{
		[]byte("BUG kmalloc-"),
		[]oopsFormat{
			{
				compile("BUG kmalloc-.*: Object already free"),
				nil,
				"BUG: Object already free",
				false,
			},
		},
		[]*regexp.Regexp{},
	},
	&oops{
		[]byte("divide error:"),
		[]oopsFormat{
			{
				compile("divide error: (?:.*\\n)+?.*RIP: [0-9]+:{{PC}} +{{PC}} +{{FUNC}}"),
				nil,
				"divide error in %[1]v",
				false,
			},
			{
				compile("divide error: (?:.*\\n)+?.*RIP: [0-9]+:{{FUNC}}"),
				nil,
				"divide error in %[1]v",
				false,
			},
			{
				// If we failed to extract function name where the fault happened,
				// the report is most likely truncated.
				compile("divide error"),
				nil,
				"divide error",
				true,
			},
		},
		[]*regexp.Regexp{},
	},
	&oops{
		[]byte("invalid opcode:"),
		[]oopsFormat{
			{
				compile("invalid opcode: (?:.*\\n)+?.*RIP: [0-9]+:{{PC}} +{{PC}} +{{FUNC}}"),
				nil,
				"invalid opcode in %[1]v",
				false,
			},
			{
				compile("invalid opcode: (?:.*\\n)+?.*RIP: [0-9]+:{{FUNC}}"),
				nil,
				"invalid opcode in %[1]v",
				false,
			},
			{
				// If we failed to extract function name where the fault happened,
				// the report is most likely truncated.
				compile("invalid opcode"),
				nil,
				"invalid opcode",
				true,
			},
		},
		[]*regexp.Regexp{},
	},
	&oops{
		[]byte("unreferenced object"),
		[]oopsFormat{
			{
				compile("unreferenced object {{ADDR}} \\(size ([0-9]+)\\):(?:.*\n.*)+backtrace:.*\n.*{{PC}}.*\n.*{{PC}}.*\n.*{{PC}} {{FUNC}}"),
				nil,
				"memory leak in %[2]v (size %[1]v)",
				false,
			},
		},
		[]*regexp.Regexp{},
	},
	&oops{
		[]byte("UBSAN:"),
		[]oopsFormat{},
		[]*regexp.Regexp{},
	},
}
