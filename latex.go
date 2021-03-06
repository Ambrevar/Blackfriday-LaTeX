// Copyright © 2013-2016 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

// Package latex is a LaTeX renderer for the Blackfriday Markdown processor.
package latex

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"

	bf "gopkg.in/russross/blackfriday.v2"
)

// Renderer is a type that implements the Renderer interface for LaTeX
// output.
type Renderer struct {
	w bytes.Buffer

	// Flags allow customizing this renderer's behavior.
	Flags Flag

	// The document author displayed by the `\maketitle` command.
	// This will only display if the `Titleblock` extension is on and a title is
	// present.
	Author string

	// The languages to be used by the `babel` package.
	// Languages must be comma-spearated.
	Languages string

	// If text is within quotes.
	quoted bool
}

// Flag controls the options of the renderer.
type Flag int

const (
	FlagsNone Flag = 0

	// CompletePage generates a complete LaTeX document, preamble included.
	CompletePage Flag = 1 << iota

	// ChapterTitle uses the titleblock (if the extension is on) as chapter title.
	// Ignored when CompletePage is on.
	ChapterTitle

	// No paragraph indentation.
	NoParIndent

	SkipLinks // Never link.
	Safelink  // Only link to trusted protocols.

	TOC // Generate the table of content.
)

var cellAlignment = [4]byte{
	0: 'l',
	bf.TableAlignmentLeft:   'l',
	bf.TableAlignmentRight:  'r',
	bf.TableAlignmentCenter: 'c',
}

var latexEscaper = [256][]byte{
	'#':  []byte(`\#`),
	'$':  []byte(`\$`),
	'%':  []byte(`\%`),
	'&':  []byte(`\&`),
	'\\': []byte(`\textbackslash{}`),
	'_':  []byte(`\_`),
	'{':  []byte(`\{`),
	'}':  []byte(`\}`),
	'~':  []byte(`\~`),
	'"':  []byte(`\enquote{`),
}

func (r *Renderer) esc(text []byte) {
	for i := 0; i < len(text); i++ {
		// directly copy normal characters
		org := i

		for i < len(text) && latexEscaper[text[i]] == nil {
			i++
		}

		if i > org {
			r.w.Write(text[org:i])
			if i >= len(text) {
				break
			}
		}

		// escape a character
		if text[i] == '"' {
			if r.quoted {
				r.w.WriteByte('}')
				r.quoted = false
			} else {
				r.w.Write(latexEscaper[text[i]])
				r.quoted = true
			}
		} else {
			r.w.Write(latexEscaper[text[i]])
		}
	}
}

func languageAttr(info []byte) []byte {
	if len(info) == 0 {
		return nil
	}
	endOfLang := bytes.IndexAny(info, "\t ")
	if endOfLang < 0 {
		return info
	}
	return info[:endOfLang]
}

func (r *Renderer) env(environment string, entering bool) {
	if entering {
		r.w.WriteString(`\begin{` + environment + "}\n")
	} else {
		r.w.WriteString(`\end{` + environment + "}\n\n")
	}
}

func (r *Renderer) cmd(command string, entering bool) {
	if entering {
		r.w.WriteString(`\` + command + `{`)
	} else {
		r.w.WriteByte('}')
	}
}

// Return the first ASCII character that is not in 'text'.
// The resulting delimiter cannot be '*' nor space.
func getDelimiter(text []byte) byte {
	delimiters := make([]bool, 256)
	for _, v := range text {
		delimiters[v] = true
	}
	// '!' is the character after space in the ASCII encoding.
	for k := byte('!'); k < byte('*'); k++ {
		if !delimiters[k] {
			return k
		}
	}
	// '+' is the character after '*' in the ASCII encoding.
	for k := byte('+'); k < 128; k++ {
		if !delimiters[k] {
			return k
		}
	}
	return 0
}

func hasPrefixCaseInsensitive(s, prefix []byte) bool {
	if len(s) < len(prefix) {
		return false
	}
	delta := byte('a' - 'A')
	for i, b := range prefix {
		if b != s[i] && b != s[i]+delta {
			return false
		}
	}
	return true
}

// RenderNode renders a single node.
// As a rule of thumb to enforce consistency, each node is responsible for
// appending the needed line breaks. Line breaks are never prepended.
func (r *Renderer) RenderNode(w io.Writer, node *bf.Node, entering bool) bf.WalkStatus {
	switch node.Type {

	case bf.BlockQuote:
		r.env("quotation", entering)

	case bf.Code:
		// TODO: Reach a consensus for math syntax.
		if bytes.HasPrefix(node.Literal, []byte("$$ ")) {
			// Inline math
			r.w.WriteByte('$')
			r.w.Write(node.Literal[3:])
			r.w.WriteByte('$')
			break
		}
		// 'lstinline' needs an ASCII delimiter that is not in the node content.
		// TODO: Find a more elegant fallback for when the code lists all ASCII characters.
		delimiter := getDelimiter(node.Literal)
		r.w.WriteString(`\lstinline`)
		if delimiter != 0 {
			r.w.WriteByte(delimiter)
			r.w.Write(node.Literal)
			r.w.WriteByte(delimiter)
		} else {
			r.w.WriteString("!<RENDERING ERROR: no delimiter found>!")
		}

	case bf.CodeBlock:
		lang := languageAttr(node.Info)
		if bytes.Compare(lang, []byte("math")) == 0 {
			r.w.WriteString("\\[\n")
			r.w.Write(node.Literal)
			r.w.WriteString("\\]\n\n")
			break
		}
		r.w.WriteString(`\begin{lstlisting}[language=`)
		r.w.Write(lang)
		r.w.WriteString("]\n")
		r.w.Write(node.Literal)
		r.w.WriteString(`\end{lstlisting}` + "\n\n")

	case bf.Del:
		r.cmd("sout", entering)

	case bf.Document:
		break

	case bf.Emph:
		r.cmd("emph", entering)

	case bf.Hardbreak:
		r.w.WriteString(`~\\` + "\n")

	case bf.Heading:
		if node.IsTitleblock {
			// Nothing to print but its children.
			break
		}
		if entering {
			switch node.Level {
			case 1:
				r.w.WriteString(`\section{`)
			case 2:
				r.w.WriteString(`\subsection{`)
			case 3:
				r.w.WriteString(`\subsubsection{`)
			case 4:
				r.w.WriteString(`\paragraph{`)
			case 5:
				r.w.WriteString(`\subparagraph{`)
			case 6:
				r.w.WriteString(`\textbf{`)
			}
		} else {
			r.w.WriteByte('}')
			switch node.Level {
			// Paragraph need no newline.
			case 1, 2, 3:
				r.w.WriteByte('\n')
			default:
				r.w.WriteByte(' ')
			}
		}

	case bf.HTMLBlock:
		// HTML code makes no sense in LaTeX.
		break

	case bf.HTMLSpan:
		// HTML code makes no sense in LaTeX.
		break

	case bf.HorizontalRule:
		r.w.WriteString(`\HRule{}` + "\n")

	case bf.Image:
		if entering {
			dest := node.LinkData.Destination
			if hasPrefixCaseInsensitive(dest, []byte("http://")) || hasPrefixCaseInsensitive(dest, []byte("https://")) {
				r.w.WriteString(`\url{`)
				r.w.Write(dest)
				r.w.WriteByte('}')
				return bf.SkipChildren
			}
			if node.LinkData.Title != nil {
				r.w.WriteString(`\begin{figure}[!ht]` + "\n")
			}
			r.w.WriteString(`\begin{center}` + "\n")
			// Trim extension so that LaTeX loads the most appropriate file.
			ext := filepath.Ext(string(dest))
			dest = dest[:len(dest)-len(ext)]
			r.w.WriteString(`\includegraphics[max width=\textwidth, max height=\textheight]{`)
			r.w.Write(dest)
			r.w.WriteString("}\n" + `\end{center}` + "\n")
			if node.LinkData.Title != nil {
				r.w.WriteString(`\caption{`)
				r.w.Write(node.LinkData.Title)
				r.w.WriteString("}\n" + `\end{figure}` + "\n")
			}
		}
		return bf.SkipChildren

	case bf.Item:
		if entering {
			if node.ListFlags&bf.ListTypeTerm != 0 {
				r.w.WriteString(`\item [`)
			} else if node.ListFlags&bf.ListTypeDefinition == 0 {
				r.w.WriteString(`\item `)
			}
		} else {
			if node.ListFlags&bf.ListTypeTerm != 0 {
				r.w.WriteString("] ")
			}
		}

	case bf.Link:
		// TODO: Relative links do not make sense in LaTeX. Print a warning?
		dest := node.LinkData.Destination

		// Raw URI
		if needSkipLink(r.Flags, dest) {
			if node.FirstChild != node.LastChild || node.FirstChild.Type != bf.Text || bytes.Compare(dest, node.FirstChild.Literal) != 0 {
				if !entering {
					r.w.WriteString(`\footnote{\nolinkurl{`)
					r.w.Write(dest)
					r.w.WriteString(`}}`)
				}
				break
			}
			// Link content (only one Text child) and destination are identical (e.g.
			// with autolink).
			r.w.WriteString(`\nolinkurl{`)
			r.w.Write(dest)
			r.w.WriteByte('}')
			return bf.SkipChildren
		}

		// Footnotes
		if node.NoteID != 0 {
			if entering {
				r.w.WriteString(`\footnote{`)
				w := bytes.Buffer{}
				footnoteNode := node.LinkData.Footnote
				footnoteNode.Walk(func(node *bf.Node, entering bool) bf.WalkStatus {
					if node == footnoteNode {
						return bf.GoToNext
					}
					return r.RenderNode(&w, node, entering)
				})
				r.w.Write(w.Bytes())
				r.w.WriteString(`}`)
			}
			break
		}

		// Normal link
		if entering {
			r.w.WriteString(`\href{`)
			r.w.Write(dest)
			r.w.WriteString(`}{`)
		} else {
			r.w.WriteByte('}')
		}

	case bf.List:
		if node.IsFootnotesList {
			// The footnote list is not needed for LaTeX as the footnotes are rendered
			// directly from the links.
			return bf.SkipChildren
		}
		listType := "itemize"
		if node.ListFlags&bf.ListTypeOrdered != 0 {
			listType = "enumerate"
		}
		if node.ListFlags&bf.ListTypeDefinition != 0 {
			listType = "description"
		}
		r.env(listType, entering)

	case bf.Paragraph:
		if !entering {
			// If paragraph is the term of a definition list, don't insert new lines.
			if node.Parent.Type != bf.Item || node.Parent.ListFlags&bf.ListTypeTerm == 0 {
				r.w.WriteByte('\n')
				// Don't insert an additional linebreak after last node of an item, a quote, etc.
				if node.Next != nil {
					r.w.WriteByte('\n')
				}
			}
		}

	case bf.Softbreak:
		// TODO: Upstream does not use it. If status changes, linebreaking should be
		// updated.
		break

	case bf.Strong:
		r.cmd("textbf", entering)

	case bf.Table:
		if entering {
			r.w.WriteString(`\begin{center}` + "\n" + `\begin{tabular}{`)
			node.Walk(func(c *bf.Node, entering bool) bf.WalkStatus {
				if c.Type == bf.TableCell && entering {
					for cell := c; cell != nil; cell = cell.Next {
						r.w.WriteByte(cellAlignment[cell.Align])
					}
					return bf.Terminate
				}
				return bf.GoToNext
			})
			r.w.WriteString("}\n")
		} else {
			r.w.WriteString(`\end{tabular}` + "\n" + `\end{center}` + "\n\n")
		}

	case bf.TableBody:
		// Nothing to do here.
		break

	case bf.TableCell:
		if node.IsHeader {
			r.cmd("textbf", entering)
		}
		if !entering && node.Next != nil {
			r.w.WriteString(" & ")
		}

	case bf.TableHead:
		if !entering {
			r.w.WriteString(`\hline` + "\n")
		}

	case bf.TableRow:
		if !entering {
			r.w.WriteString(` \\` + "\n")
		}

	case bf.Text:
		r.esc(node.Literal)
		break

	default:
		panic("Unknown node type " + node.Type.String())
	}
	return bf.GoToNext
}

// Get title: concatenate all Text children of Titleblock.
func getTitle(ast *bf.Node) []byte {
	titleRenderer := Renderer{}

	ast.Walk(func(node *bf.Node, entering bool) bf.WalkStatus {
		if node.Type == bf.Heading && node.HeadingData.IsTitleblock && entering {
			node.Walk(func(c *bf.Node, entering bool) bf.WalkStatus {
				return titleRenderer.RenderNode(&titleRenderer.w, c, entering)
			})
			return bf.Terminate
		}
		return bf.GoToNext
	})
	return titleRenderer.w.Bytes()
}

func hasFigures(ast *bf.Node) bool {
	result := false
	ast.Walk(func(node *bf.Node, entering bool) bf.WalkStatus {
		if node.Type == bf.Image && node.LinkData.Title != nil {
			result = true
			return bf.Terminate
		}
		return bf.GoToNext
	})
	return result
}

// RenderHeader prints the LaTeX preamble if CompletePage is on.
func (r *Renderer) RenderHeader(w io.Writer, ast *bf.Node) {
	var title string

	if r.Flags&CompletePage != 0 {
		title = string(getTitle(ast))

		// TODO: Color source code and links?
		io.WriteString(w, `\documentclass{article}

\usepackage[utf8]{inputenc}
\usepackage[T1]{fontenc}
\usepackage{lmodern}
\usepackage{marvosym}
\usepackage{textcomp}
\DeclareUnicodeCharacter{20AC}{\EUR{}}
\DeclareUnicodeCharacter{2260}{\neq}
\DeclareUnicodeCharacter{2264}{\leq}
\DeclareUnicodeCharacter{2265}{\geq}
\DeclareUnicodeCharacter{22C5}{\cdot}
\DeclareUnicodeCharacter{A0}{~}
\DeclareUnicodeCharacter{B1}{\pm}
\DeclareUnicodeCharacter{D7}{\times}

\usepackage{amsmath}
\usepackage[export]{adjustbox} % loads also graphicx
\usepackage{listings}
\usepackage[margin=1in]{geometry}
\usepackage{verbatim}
\usepackage[normalem]{ulem}
\usepackage{hyperref}

\lstset{
	numbers=left,
	breaklines=true,
	xleftmargin=2\baselineskip,
	showstringspaces=false,
	basicstyle=\ttfamily,
	keywordstyle=\bfseries\color{green!40!black},
	commentstyle=\itshape\color{purple!40!black},
	stringstyle=\color{orange},
	numberstyle=\ttfamily,
	literate=
	{á}{{\'a}}1 {é}{{\'e}}1 {í}{{\'i}}1 {ó}{{\'o}}1 {ú}{{\'u}}1
	{Á}{{\'A}}1 {É}{{\'E}}1 {Í}{{\'I}}1 {Ó}{{\'O}}1 {Ú}{{\'U}}1
	`)
		io.WriteString(w,
			"{à}{{\\`a}}1 {è}{{\\`e}}1 {ì}{{\\`i}}1 {ò}{{\\`o}}1 {ù}{{\\`u}}1"+
				"\n\t"+
				"{À}{{\\`A}}1 {È}{{\\'E}}1 {Ì}{{\\`I}}1 {Ò}{{\\`O}}1 {Ù}{{\\`U}}1")
		io.WriteString(w, `
	{ä}{{\"a}}1 {ë}{{\"e}}1 {ï}{{\"i}}1 {ö}{{\"o}}1 {ü}{{\"u}}1
	{Ä}{{\"A}}1 {Ë}{{\"E}}1 {Ï}{{\"I}}1 {Ö}{{\"O}}1 {Ü}{{\"U}}1
	{â}{{\^a}}1 {ê}{{\^e}}1 {î}{{\^i}}1 {ô}{{\^o}}1 {û}{{\^u}}1
	{Â}{{\^A}}1 {Ê}{{\^E}}1 {Î}{{\^I}}1 {Ô}{{\^O}}1 {Û}{{\^U}}1
	{œ}{{\oe}}1 {Œ}{{\OE}}1 {æ}{{\ae}}1 {Æ}{{\AE}}1 {ß}{{\ss}}1
	{ű}{{\H{u}}}1 {Ű}{{\H{U}}}1 {ő}{{\H{o}}}1 {Ő}{{\H{O}}}1
	{ç}{{\c c}}1 {Ç}{{\c C}}1 {ø}{{\o}}1 {å}{{\r a}}1 {Å}{{\r A}}1
	{€}{{\EUR}}1 {£}{{\pounds}}1
}
`)

		if r.Languages != "" {
			io.WriteString(w, "\n"+`\usepackage[`+r.Languages+`]{babel}`+"\n")
		}

		io.WriteString(w, `\usepackage{csquotes}

\hypersetup{colorlinks,
	citecolor=black,
	filecolor=black,
	linkcolor=black,
	linktoc=page,
	urlcolor=black,
	pdfstartview=FitH,
	breaklinks=true,
	pdfauthor={Blackfriday Markdown Processor v`)
		io.WriteString(w, bf.Version)
		io.WriteString(w, `},
}

\newcommand{\HRule}{\rule{\linewidth}{0.5mm}}
\addtolength{\parskip}{0.5\baselineskip}
`)

		if r.Flags&NoParIndent != 0 {
			io.WriteString(w, `\parindent=0pt
`)
		}

		if title != "" {
			io.WriteString(w, `
\title{`+title+`}
\author{`+r.Author+`}
`)
		}

		io.WriteString(w, `
\begin{document}
`)

		if title != "" {
			r.w.WriteString(`
\maketitle
`)
			if r.Flags&TOC != 0 {
				r.w.WriteString(`\vfill
\thispagestyle{empty}

\tableofcontents
`)
				if hasFigures(ast) {
					io.WriteString(w, `\listoffigures
`)
				}
				io.WriteString(w, `\clearpage
`)
			}
		}

		io.WriteString(w, "\n\n")
	} else if r.Flags&ChapterTitle != 0 && strings.TrimSpace(title) != "" {
		io.WriteString(w, `\chapter{`+title+"}\n\n")
	}
}

// RenderHeader prints the '\end{document}' if CompletePage is on.
func (r *Renderer) RenderFooter(w io.Writer, ast *bf.Node) {
	if r.Flags&CompletePage != 0 {
		io.WriteString(w, `\end{document}`+"\n")
	}
}

// Render prints out the whole document from the ast, header and footer included.
func (r *Renderer) Render(ast *bf.Node) []byte {
	r.RenderHeader(&r.w, ast)
	ast.Walk(func(node *bf.Node, entering bool) bf.WalkStatus {
		if node.Type == bf.Heading && node.HeadingData.IsTitleblock {
			return bf.SkipChildren
		}
		return r.RenderNode(&r.w, node, entering)
	})

	r.RenderFooter(&r.w, ast)
	return r.w.Bytes()
}

// Run prints out the whole document with CompletePage and TOC flags enabled.
func Run(input []byte, opts ...bf.Option) []byte {
	renderer := &Renderer{Flags: CompletePage | TOC}

	optList := []bf.Option{bf.WithRenderer(renderer), bf.WithExtensions(bf.CommonExtensions)}
	optList = append(optList, opts...)
	parser := bf.New(optList...)
	ast := parser.Parse([]byte(input))
	return renderer.Render(ast)
}
