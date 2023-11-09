package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/samber/lo"
	"github.com/spf13/pflag"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func main() {
	if err := mainE(); err != nil {
		fmt.Printf("execution failed: %s\n", err.Error())
		os.Exit(1)
	}
}

func mainE() error {
	var (
		binary string
		skipSymbols bool
		format string
	)

	pflag.StringVarP(&binary, "binary", "b", "", "Set the binary that will be analyzed *required*")
	pflag.BoolVarP(&skipSymbols, "skip-symbols", "s", false, "Skip emitting granular symbol data")
	pflag.StringVarP(&format, "format", "f", "json", "Select the output format from [json]")
	pflag.Parse()

	binary = strings.TrimSpace(binary)
	if binary == "" {
		return fmt.Errorf("binary argument is required")
	}

	cmd := exec.Command("go", "tool", "nm", "-size", binary)

	outputBytes, err := cmd.Output()
	if err != nil {
		return err
	}

	re, err := regexp.Compile(`^\s*([a-fA-F0-9]+)?\s*([0-9]+)\s*([a-zA-Z\-\?])\s*(.+)$`)
	if err != nil {
		return err
	}

	lines := strings.Split(string(outputBytes), "\n")

	symbols := make([]symbol, 0, len(lines))

	for _, line := range lines {
		match := re.FindStringSubmatch(line)
		if match == nil {
			fmt.Fprintf(os.Stderr, "failed to find type symbol in line: %s\n", line)
			continue
		}

		ret := symbol{}

		if match[1] != "" {
			if addr, err := strconv.ParseInt(match[1], 16, 64); err == nil {
				ret.Address = addr
			}
		}

		if match[2] != "" {
			if siz, err := strconv.ParseInt(match[2], 10, 64); err == nil {
				ret.Size = siz
			}
		}

		ret.Type = match[3]

		name := match[4]

		if strings.HasPrefix(name, "go:") || strings.HasPrefix(name, "type:") {
			continue
		}

		// if it's probably a Go symbol from a repo
		if strings.ContainsRune(name, '/') {
			lastPathSep := strings.LastIndex(name, "/")

			nameDotIdx := strings.IndexRune(name[lastPathSep:], '.')
			if nameDotIdx > -1 {
				ret.Package = name[0:(lastPathSep + nameDotIdx)]
				ret.PackageChunks = strings.Split(ret.Package, "/")
				ret.Func = name[(lastPathSep+nameDotIdx+1):]
			}
		} else if packageDotIndex := strings.IndexRune(name, '.'); packageDotIndex > -1 {
			ret.Package = name[0:packageDotIndex]
			ret.PackageChunks = []string{ret.Package}
			ret.Func = name[(packageDotIndex+1):]
		} else {
			ret.Func = name
		}

		symbols = append(symbols, ret)
	}

	packageGroups := lo.GroupBy(symbols, func(in symbol) string {
		return in.Package
	})

	root := &packageTree{
		Package: binary,
	}

	for pkg, symbols := range packageGroups {
		if pkg == "" || len(symbols) < 1{
			continue
		}

		addToTree(root, 0, symbols[0].PackageChunks, symbols)
	}

	if skipSymbols {
		root.dropSymbols()
	}

	switch strings.ToLower(format) {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")

		return enc.Encode(root)
	case "ui":
		return renderUI(root)
	}

	return nil
}

func addToTree(node *packageTree, index int, chunks []string, symbols []symbol) int64 {
	if len(chunks) - index > 0 {
		if node.Children == nil {
			node.Children = map[string]*packageTree{}
		}

		chunk := chunks[index]

		if _, found := node.Children[chunk]; !found {
			node.Children[chunk] = &packageTree{
				Package:       strings.Join(chunks[:index+1], "/"),
				packageChunks: chunks[:index+1],
			}

			// fmt.Printf("inserted %v under %s\n", node.Children[chunk], node.Package)
		}

		subSize := addToTree(node.Children[chunk], index+1, chunks, symbols)

		node.AccumulatedSize = node.AccumulatedSize + subSize

		return subSize
	} else { // no chunks left, this is just the leaves of the tree
		// fmt.Printf("inserting %d children to %v\n", len(symbols), node.Package)

		node.Symbols = lo.Map(symbols, func(in symbol, _ int) symbolSummary {
			return in.ToSummary()
		})

		node.PackageSize = lo.SumBy(symbols, func(in symbol) int64 {
			return in.Size
		})

		node.AccumulatedSize = node.AccumulatedSize + node.PackageSize

		return node.PackageSize
	}
}

type packageTree struct {
	Package         string                  `json:"package,omitempty"`
	packageChunks   []string
	PackageSize     int64                   `json:"package_size,omitempty"`
	AccumulatedSize int64                   `json:"accumulated_size,omitempty"`
	Symbols         []symbolSummary         `json:"symbols,omitempty"`
	Children        map[string]*packageTree `json:"children,omitempty"`
}

func (s *packageTree) dropSymbols() {
	s.Symbols = nil

	for _, child := range s.Children {
		if child == nil {
			continue
		}

		child.dropSymbols()
	}
}

type symbol struct {
	Address       int64    `json:"address,omitempty"`
	Size          int64    `json:"size,omitempty"`
	Type          string   `json:"type,omitempty"`
	Package       string   `json:"package,omitempty"`
	PackageChunks []string `json:"package_chunks,omitempty"`
	Func          string   `json:"func,omitempty"`
}

func (s symbol) DescSymbol() string {
	if s.Package != "" {
		return s.Package + "." + s.Func
	}

	return s.Func
}

func (s symbol) String() string {
	return fmt.Sprintf("%x\t%d\t%s\t%s\t%s", s.Address, s.Size, s.Type, s.Package, s.Func)
}

func (s symbol) ToSummary() symbolSummary {
	return symbolSummary{
		Size: s.Size,
		Type: s.Type,
		Func: s.Func,
	}
}

type symbolSummary struct {
	Size int64  `json:"size,omitempty"`
	Type string `json:"type,omitempty"`
	Func string `json:"func,omitempty"`
}

func renderUI(tree *packageTree) error {
	totalSize := float64(tree.AccumulatedSize)

	root := tview.NewTreeNode("bin " + tree.Package).SetColor(tcell.ColorRed)

	treeView := tview.NewTreeView().
		SetRoot(root).
		SetCurrentNode(root)

	add := func(target *tview.TreeNode, tree *packageTree) {
		for _, subPackage := range tree.Children {
			sizePct := (float64(subPackage.AccumulatedSize) / totalSize) * 100

			node := tview.NewTreeNode(fmt.Sprintf("pkg %s %4.2f", subPackage.Package, sizePct)).
				SetReference(subPackage).
				SetSelectable(true).
				SetColor(tcell.ColorGreen)

			target.AddChild(node)
		}

		for _, symbol := range tree.Symbols {
			sizePct := (float64(symbol.Size) / totalSize) * 100

			node := tview.NewTreeNode(fmt.Sprintf("sym %s %4.2f", symbol.Func, sizePct)).
				SetReference(symbol).
				SetSelectable(true).
				SetColor(tcell.ColorYellow)

			target.AddChild(node)
		}
	}

	add(root, tree)

	treeView.SetSelectedFunc(func(node *tview.TreeNode) {
		ref := node.GetReference()
		if ref == nil {
			return
		}

		children := node.GetChildren()
		if len(children) == 0 {
			if tree, ok := ref.(*packageTree); ok {
				add(node, tree)
			}
		} else {
			node.SetExpanded(!node.IsExpanded())
		}
	})

	return tview.NewApplication().SetRoot(treeView, true).Run()
}
