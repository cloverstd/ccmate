import { useState, useRef, useCallback, useEffect } from 'react'
import Markdown from './Markdown'

const TEMPLATE_VARS = [
  { name: 'IssueNumber', desc: 'Issue number', example: '42' },
  { name: 'IssueTitle', desc: 'Issue title', example: 'Fix login bug' },
  { name: 'IssueBody', desc: 'Issue body', example: '...' },
  { name: 'IssueLabels', desc: 'Issue labels ([]string)', example: '["bug"]' },
  { name: 'IssueUser', desc: 'Issue author', example: 'octocat' },
  { name: 'IssueLink', desc: 'Issue URL', example: 'https://github.com/...' },
  { name: 'RepoOwner', desc: 'Repo owner', example: 'acme' },
  { name: 'RepoName', desc: 'Repo name', example: 'app' },
  { name: 'RepoFullName', desc: 'Full repo name', example: 'acme/app' },
  { name: 'TaskType', desc: 'Task type', example: 'issue_implementation' },
  { name: 'BranchName', desc: 'Branch name', example: 'ccmate/issue-42-task-1' },
]

interface Props {
  label: string
  value: string
  onChange: (val: string) => void
  placeholder?: string
  rows?: number
  showVars?: boolean
}

export default function PromptEditor({ label, value, onChange, placeholder, rows = 6, showVars = false }: Props) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const [showCompletion, setShowCompletion] = useState(false)
  const [completionFilter, setCompletionFilter] = useState('')
  const [completionPos, setCompletionPos] = useState({ top: 0, left: 0 })
  const [selectedIdx, setSelectedIdx] = useState(0)

  const filteredVars = TEMPLATE_VARS.filter(v =>
    v.name.toLowerCase().includes(completionFilter.toLowerCase())
  )

  const insertVariable = useCallback((varName: string) => {
    const ta = textareaRef.current
    if (!ta) return
    const pos = ta.selectionStart
    const before = value.substring(0, pos)
    const after = value.substring(ta.selectionEnd)

    // Find the `{{` or `{{.` prefix to replace
    const match = before.match(/\{\{\.?(\w*)$/)
    if (match) {
      const start = pos - match[0].length
      const insertion = `{{.${varName}}}`
      onChange(value.substring(0, start) + insertion + after)
      setTimeout(() => {
        ta.selectionStart = ta.selectionEnd = start + insertion.length
        ta.focus()
      }, 0)
    }
    setShowCompletion(false)
  }, [value, onChange])

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (!showCompletion || filteredVars.length === 0) return
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setSelectedIdx(i => Math.min(i + 1, filteredVars.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setSelectedIdx(i => Math.max(i - 1, 0))
    } else if (e.key === 'Enter' || e.key === 'Tab') {
      e.preventDefault()
      insertVariable(filteredVars[selectedIdx].name)
    } else if (e.key === 'Escape') {
      setShowCompletion(false)
    }
  }, [showCompletion, filteredVars, selectedIdx, insertVariable])

  const handleInput = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const newVal = e.target.value
    onChange(newVal)

    if (!showVars) return

    const pos = e.target.selectionStart
    const textBefore = newVal.substring(0, pos)
    const match = textBefore.match(/\{\{\.?(\w*)$/)

    if (match) {
      setCompletionFilter(match[1] || '')
      setSelectedIdx(0)
      // Position completion dropdown near cursor
      const lineHeight = 20
      const lines = textBefore.split('\n')
      const row = lines.length - 1
      setCompletionPos({ top: (row + 1) * lineHeight + 8, left: 8 })
      setShowCompletion(true)
    } else {
      setShowCompletion(false)
    }
  }, [onChange, showVars])

  useEffect(() => {
    if (showCompletion && filteredVars.length === 0) setShowCompletion(false)
  }, [showCompletion, filteredVars.length])

  return (
    <div>
      <label className="text-xs font-medium text-gray-600 mb-1 block">{label}</label>
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
        {/* Editor */}
        <div className="relative">
          <textarea
            ref={textareaRef}
            value={value}
            onChange={handleInput}
            onKeyDown={handleKeyDown}
            onBlur={() => setTimeout(() => setShowCompletion(false), 150)}
            placeholder={placeholder}
            rows={rows}
            className="w-full px-3 py-2 border rounded-lg text-sm font-mono resize-y"
          />
          {showCompletion && filteredVars.length > 0 && (
            <div
              className="absolute z-10 bg-white border border-gray-200 rounded-lg shadow-lg max-h-48 overflow-y-auto w-72"
              style={{ top: completionPos.top + 32, left: completionPos.left }}
            >
              {filteredVars.map((v, i) => (
                <button
                  key={v.name}
                  onMouseDown={(e) => { e.preventDefault(); insertVariable(v.name) }}
                  className={`w-full text-left px-3 py-1.5 text-sm flex items-center gap-2 ${i === selectedIdx ? 'bg-blue-50 text-blue-700' : 'hover:bg-gray-50'}`}
                >
                  <code className="text-xs font-mono bg-gray-100 px-1 rounded">.{v.name}</code>
                  <span className="text-xs text-gray-500 truncate">{v.desc}</span>
                </button>
              ))}
            </div>
          )}
        </div>
        {/* Live preview */}
        <div className="border rounded-lg p-3 bg-gray-50 min-h-[8rem] overflow-auto">
          <div className="text-xs text-gray-400 mb-2">Preview</div>
          {value ? <Markdown>{value}</Markdown> : <p className="text-sm text-gray-400 italic">No content</p>}
        </div>
      </div>
    </div>
  )
}
