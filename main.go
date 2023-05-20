package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/mitchellh/colorstring"
)

// Instruction types
const (
	ADD_SUB     int = iota // +/-
	PTR_MOV                // </>
	JMP_ZER                // [
	JMP_NOT_ZER            // ]
	PUT_CHR                // .
	RAD_CHR                // ,
	CLR                    // [-]
	MUL_CPY                // [-<++>]
	SCN_RGT                // [>]
	SCN_LFT                // [<]
)

type Instruction struct {
	Type    int
	Data    int
	AuxData int
	Offset  int
}

// Utility functions

func fold(code *string, i *int, char byte) int {
	var count int = 1
	for *i < len(*code)-1 && (*code)[*i+1] == char {
		count++
		*i++
	}

	return count
}

func processBalanced(s, char1, char2 string) string {
	var total int = strings.Count(s, char1) - strings.Count(s, char2)
	if total > 0 {
		return strings.Repeat(char1, total)
	} else if total < 0 {
		return strings.Repeat(char2, -total)
	} else {
		return ""
	}
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

// Main code

func dumpMem(cells *[]byte, cellptr *int) {
	var lastNonEmpty int = 0
	for x := len(*cells) - 1; x > 0; x-- {
		if (*cells)[x] != 0 {
			lastNonEmpty = x
			break
		}
	}
	fmt.Println("Memory dump:")
	colorstring.Println("[underline]         0   1   2   3   4   5   6   7   8   9[default]")
	var row int = 0
	for x := 0; x <= max(lastNonEmpty, *cellptr); x++ {
		if x%10 == 0 {
			if row != 0 {
				fmt.Println()
			}
			fmt.Print(row, strings.Repeat(" ", 9-len(fmt.Sprint(row))))
			row += 10
		}
		if x == *cellptr {
			colorstring.Printf("[green]%d[default]%s", (*cells)[x], strings.Repeat(" ", 4-len(fmt.Sprint((*cells)[x]))))
		} else {
			fmt.Print((*cells)[x], strings.Repeat(" ", 4-len(fmt.Sprint((*cells)[x]))))
		}
	}
	fmt.Println()
}

func execute(cells *[]byte, cellptr *int, code string, printStats bool, dumpMemory bool, optimize bool) int {
	var start = time.Now()

	//* Optimize
	// Remove useless characters
	code = regexp.MustCompile(`[^\+\-\>\<\.\,\]\[]`).ReplaceAllString(code, "")

	// Remove NOPs
	code = regexp.MustCompile(`[+-]{2,}`).ReplaceAllStringFunc(code, func(s string) string { return processBalanced(s, "+", "-") })
	code = regexp.MustCompile(`[><]{2,}`).ReplaceAllStringFunc(code, func(s string) string { return processBalanced(s, ">", "<") })

	var copyloopCounter int
	var copyloopMap = make([]int, 0)
	var copyloopMulMap = make([]int, 0)

	var scanloopCounter int
	var scanloopMap = make([]int, 0)

	if optimize {
		// Clearloop optimization
		code = regexp.MustCompile(`[C+-]*(?:\[[+-]+\])+\.*`).ReplaceAllString(code, "C") // Also delete any modifications to cell that is being cleared

		// Scanloop optimization
		code = regexp.MustCompile(`\[>+\]`).ReplaceAllStringFunc(code, func(s string) string {
			scanloopMap = append(scanloopMap, strings.Count(s, ">"))
			return "R"
		})
		code = regexp.MustCompile(`\[<+\]`).ReplaceAllStringFunc(code, func(s string) string {
			scanloopMap = append(scanloopMap, strings.Count(s, "<"))
			return "L"
		})

		// Don't clear or print if cell is known zero
		code = regexp.MustCompile(`[RL]+C|[CRL]+\.+`).ReplaceAllString(code, "")

		// Don't update cells if they are immediately overwritten by stdin
		code = regexp.MustCompile(`[+-C]+,`).ReplaceAllString(code, ",")

		// Multiloops/copyloops optimization
		code = regexp.MustCompile(`\[-(?:[<>]+\++)+[<>]+\]|\[(?:[<>]+\++)+[<>]+-\]`).ReplaceAllStringFunc(code, func(s string) string {
			var numOfCopies int = 0
			var offset int = 0
			if strings.Count(s, ">")-strings.Count(s, "<") == 0 {
				for _, v := range regexp.MustCompile(`[<>]+\++`).FindAllString(s, -1) {
					offset += -strings.Count(v, "<") + strings.Count(v, ">")
					copyloopMap = append(copyloopMap, offset)
					copyloopMulMap = append(copyloopMulMap, strings.Count(v, "+"))
					numOfCopies++
				}
				return fmt.Sprintf("%sC", strings.Repeat("P", numOfCopies))
			} else {
				return s
			}
		})
	}

	// Compile & link loops
	var instructions = make([]Instruction, 0)
	var braceStack = make([]int, 0)
	var offset = 0
	for i := 0; i < len(code); i++ {
		var newInstruction Instruction
		switch code[i] {
		case '+':
			newInstruction = Instruction{ADD_SUB, fold(&code, &i, '+'), 0, offset}
		case '-':
			newInstruction = Instruction{ADD_SUB, -fold(&code, &i, '-'), 0, offset}
		case '>':
			if len(braceStack) == 0 {
				offset += fold(&code, &i, '>')
				continue
			}
			newInstruction = Instruction{PTR_MOV, fold(&code, &i, '>'), 0, 0}
		case '<':
			if len(braceStack) == 0 {
				offset += -fold(&code, &i, '<')
				continue
			}
			newInstruction = Instruction{PTR_MOV, -fold(&code, &i, '<'), 0, 0}
		case '[':
			if offset != 0 {
				newInstruction = Instruction{PTR_MOV, offset, 0, 0}
				i--
				offset = 0
			} else {
				braceStack = append(braceStack, len(instructions))
				newInstruction = Instruction{JMP_ZER, 0, 0, 0}
			}
		case ']':
			if len(braceStack) == 0 {
				return 1
			}
			start := braceStack[len(braceStack)-1]
			braceStack = braceStack[:len(braceStack)-1]
			instructions[start].Data = len(instructions)
			newInstruction = Instruction{JMP_NOT_ZER, start, 0, 0}
		case '.':
			newInstruction = Instruction{PUT_CHR, fold(&code, &i, '.'), 0, offset}
		case ',':
			newInstruction = Instruction{RAD_CHR, 0, 0, offset}
		case 'C':
			newInstruction = Instruction{CLR, 0, 0, offset}
		case 'P':
			newInstruction = Instruction{MUL_CPY, copyloopMap[copyloopCounter], copyloopMulMap[copyloopCounter], offset}
			copyloopCounter++
		case 'R':
			if offset != 0 {
				newInstruction = Instruction{PTR_MOV, offset, 0, 0}
				i--
				offset = 0
			} else {
				newInstruction = Instruction{SCN_RGT, scanloopMap[scanloopCounter], 0, 0}
				scanloopCounter++
			}
		case 'L':
			if offset != 0 {
				newInstruction = Instruction{PTR_MOV, offset, 0, 0}
				i--
				offset = 0
			} else {
				newInstruction = Instruction{SCN_LFT, scanloopMap[scanloopCounter], 0, 0}
				scanloopCounter++
			}
		}
		instructions = append(instructions, newInstruction)
	}

	// Update pointer so that memory dump works even when all instructions are offset only
	if offset != 0 {
		instructions = append(instructions, Instruction{PTR_MOV, offset, 0, 0})
	}

	// *WIP*: Good error messages
	if len(braceStack) != 0 {
		/*for x := 0; x < len(tBraceStack); x++ {
		}*/
		return 2
	}

	var compilerTime = time.Since(start)

	var lCellptr = *cellptr
	var lCells = *cells
	for i := 0; i < len(instructions); i++ {
		var currentInstruction = instructions[i]

		switch currentInstruction.Type {
		case ADD_SUB:
			lCells[lCellptr+currentInstruction.Offset] = byte(int(lCells[lCellptr+currentInstruction.Offset]) + currentInstruction.Data)
		case PTR_MOV:
			lCellptr += currentInstruction.Data
		case JMP_ZER:
			if lCells[lCellptr+currentInstruction.Offset] == 0 {
				i = currentInstruction.Data
			}
		case JMP_NOT_ZER:
			if lCells[lCellptr+currentInstruction.Offset] != 0 {
				i = currentInstruction.Data
			}
		case PUT_CHR:
			fmt.Print(strings.Repeat(string(lCells[lCellptr+currentInstruction.Offset]), currentInstruction.Data))
		/*case RAD_CHR:
		// TODO: Fix this
		var b = make([]byte, 1)
		os.Stdin.Read(b)
		lCells[lCellptr+currentInstruction.Offset] = b[0]*/
		case CLR:
			lCells[lCellptr+currentInstruction.Offset] = 0
		case MUL_CPY:
			if lCells[lCellptr+currentInstruction.Offset] != 0 {
				lCells[lCellptr+currentInstruction.Data+currentInstruction.Offset] = byte(int(lCells[lCellptr+currentInstruction.Data+currentInstruction.Offset]) + int(lCells[lCellptr+currentInstruction.Offset])*currentInstruction.AuxData)
			}
		case SCN_RGT:
			for ; lCellptr < len(lCells) && lCells[lCellptr] != 0; lCellptr += currentInstruction.Data {
			}
		case SCN_LFT:
			for ; lCellptr > 0 && lCells[lCellptr] != 0; lCellptr -= currentInstruction.Data {
			}
		}
	}

	if printStats {
		var vmTime = time.Since(start) - compilerTime
		fmt.Printf("Execution time: %s (VM: %s, compiler: %s)\n", (compilerTime + vmTime).String(), vmTime.String(), compilerTime.String())
	}

	*cells = lCells
	*cellptr = lCellptr

	if dumpMemory {
		dumpMem(cells, cellptr)
	}

	return 0
}

func main() {
	var filename string
	var memorySize int
	var optimize bool
	var printStatistics bool
	var dumpMemory bool

	flag.StringVar(&filename, "i", "", "Brainfuck file to execute")
	flag.IntVar(&memorySize, "m", 30_000, "Set tape size")
	flag.BoolVar(&optimize, "o", true, "Optimize instructions (disabling can help when encountering crashes)")
	flag.BoolVar(&printStatistics, "s", false, "Print time taken and instruction count after execution")
	flag.BoolVar(&dumpMemory, "dm", false, "Dump memory after execution (doesn't do anything when starting to REPL mode)")

	flag.Parse()

	var cellptr = new(int)
	var cells = make([]byte, memorySize)

	if filename != "" {
		var data, err = os.ReadFile(filename)
		if err == nil {
			switch execute(&cells, cellptr, string(data), printStatistics, dumpMemory, optimize) {
			case 1:
				colorstring.Println("[_red_]ERROR:[_default_] Unmatched close bracket")
			case 2:
				colorstring.Println("[_red_]ERROR:[_default_] Unmatched open bracket")
			}
		} else {
			colorstring.Println("[_red_]ERROR:[_default_] " + err.Error())
		}
	} else {
		fmt.Println(`   _____  ____   ____  ______ `)
		fmt.Println(`  / ____|/ __ \ / __ \|  ____|`)
		fmt.Println(` | |  __| |  | | |  | | |__   `)
		fmt.Println(` | | |_ | |  | | |  | |  __|  `)
		fmt.Println(` | |__| | |__| | |__| | |     `)
		fmt.Println(`  \_____|\____/ \____/|_|     `)
		fmt.Println()
		fmt.Println("Goof - an optimizing BF VM written in Go")
		fmt.Println("Version 1.1 (REPL mode)")
		fmt.Println("Memory cells available: ", memorySize)
		colorstring.Println("Type [cyan]help[default] to see available commands.")

		for true {
			fmt.Print(">>> ")
			var repl, _ = bufio.NewReader(os.Stdin).ReadString('\n')

			if strings.HasPrefix(repl, "help") {
				// TODO: Add more commands
				colorstring.Println("[underline]General commands:[default]")
				colorstring.Println("[cyan]help[default] - Displays this list")
				colorstring.Println("[cyan]exit[default]/[cyan]quit[default] - Exits Goof")
				colorstring.Println("[underline]Memory commands:[default]")
				colorstring.Println("[cyan]clear[default] - Clears memory cells")
				colorstring.Println("[cyan]dump[default] - Displays values of memory cells, cell highlighted in [green]green[default] is the cell currently pointed to")
			} else if strings.HasPrefix(repl, "clear") {
				*cellptr = 0
				cells = make([]byte, memorySize)
			} else if strings.HasPrefix(repl, "dump") {
				dumpMem(&cells, cellptr)
			} else if strings.HasPrefix(repl, "exit") || strings.HasPrefix(repl, "quit") {
				os.Exit(0)
			} else {
				switch execute(&cells, cellptr, repl, printStatistics, dumpMemory, optimize) {
				case 1:
					colorstring.Println("[_red_]ERROR:[_default_] Unmatched close bracket")
				case 2:
					colorstring.Println("[_red_]ERROR:[_default_] Unmatched open bracket")
				}
			}
		}
	}
}
