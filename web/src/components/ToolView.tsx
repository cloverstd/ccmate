import { useState } from 'react'

interface ToolData {
  name: string
  tool_use_id?: string
  input: Record<string, unknown>
}

export function ToolCallView({ tools, text }: { tools: ToolData[]; text?: string }) {
  return (
    <div className="space-y-2">
      {text && <div className="text-sm text-gray-700">{text}</div>}
      {tools.map((tool, i) => <SingleToolView key={i} tool={tool} />)}
    </div>
  )
}

function SingleToolView({ tool }: { tool: ToolData }) {
  const desc = tool.input.description as string || ''
  const name = tool.name

  switch (name) {
    case 'Bash':
      return <BashToolView command={tool.input.command as string} description={desc} timeout={tool.input.timeout as number} />
    case 'Read':
      return <ReadToolView filePath={tool.input.file_path as string} />
    case 'Write':
      return <WriteToolView filePath={tool.input.file_path as string} content={tool.input.content as string} />
    case 'Edit':
      return <EditToolView filePath={tool.input.file_path as string} oldStr={tool.input.old_string as string} newStr={tool.input.new_string as string} />
    case 'TodoWrite':
      return <TodoToolView todos={tool.input.todos as Array<Record<string, string>>} />
    case 'Glob':
    case 'Grep':
      return <SearchToolView name={name} input={tool.input} />
    default:
      return <GenericToolView name={name} description={desc} input={tool.input} />
  }
}

function BashToolView({ command, description, timeout }: { command: string; description?: string; timeout?: number }) {
  return (
    <div className="rounded border border-gray-300 bg-gray-900 text-gray-100 text-sm overflow-hidden">
      <div className="flex items-center gap-2 px-3 py-1.5 bg-gray-800 border-b border-gray-700">
        <span className="text-xs font-bold text-green-400">$ BASH</span>
        {description && <span className="text-xs text-gray-400">{description}</span>}
        {timeout && <span className="text-xs text-gray-500 ml-auto">{timeout}ms</span>}
      </div>
      <pre className="px-3 py-2 text-xs overflow-x-auto whitespace-pre-wrap">{command}</pre>
    </div>
  )
}

function ReadToolView({ filePath }: { filePath: string }) {
  return (
    <div className="flex items-center gap-2 px-3 py-2 rounded border border-blue-200 bg-blue-50 text-sm">
      <span className="text-blue-600 text-xs font-bold font-mono">READ</span>
      <code className="text-blue-800 text-xs">{filePath}</code>
    </div>
  )
}

function WriteToolView({ filePath, content }: { filePath: string; content: string }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="rounded border border-purple-200 bg-purple-50 text-sm overflow-hidden">
      <div className="flex items-center gap-2 px-3 py-1.5">
        <span className="text-purple-600 text-xs font-bold font-mono">WRITE</span>
        <code className="text-purple-800 text-xs">{filePath}</code>
        <button onClick={() => setOpen(!open)} className="ml-auto text-xs text-purple-500 hover:text-purple-700">
          {open ? 'Hide' : `Show (${content?.length || 0} chars)`}
        </button>
      </div>
      {open && content && (
        <pre className="px-3 py-2 text-xs bg-purple-100/50 border-t border-purple-200 overflow-x-auto max-h-60 overflow-y-auto whitespace-pre-wrap">{content}</pre>
      )}
    </div>
  )
}

function EditToolView({ filePath, oldStr, newStr }: { filePath: string; oldStr?: string; newStr?: string }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="rounded border border-orange-200 bg-orange-50 text-sm overflow-hidden">
      <div className="flex items-center gap-2 px-3 py-1.5">
        <span className="text-orange-600 text-xs font-bold font-mono">EDIT</span>
        <code className="text-orange-800 text-xs">{filePath}</code>
        <button onClick={() => setOpen(!open)} className="ml-auto text-xs text-orange-500 hover:text-orange-700">
          {open ? 'Hide' : 'Show diff'}
        </button>
      </div>
      {open && (
        <div className="px-3 py-2 text-xs border-t border-orange-200 space-y-2">
          {oldStr && <div><span className="text-red-600 font-bold">-</span><pre className="inline whitespace-pre-wrap text-red-700 bg-red-50 px-1 rounded">{oldStr}</pre></div>}
          {newStr && <div><span className="text-green-600 font-bold">+</span><pre className="inline whitespace-pre-wrap text-green-700 bg-green-50 px-1 rounded">{newStr}</pre></div>}
        </div>
      )}
    </div>
  )
}

function TodoToolView({ todos }: { todos?: Array<Record<string, string>> }) {
  if (!todos) return null
  return (
    <div className="rounded border border-indigo-200 bg-indigo-50 text-sm px-3 py-2">
      <span className="text-indigo-600 text-xs font-bold font-mono">TODO</span>
      <div className="mt-1 space-y-1">
        {todos.map((t, i) => (
          <div key={i} className="flex items-center gap-2 text-xs">
            <span className={`w-4 h-4 rounded flex items-center justify-center text-[10px] ${
              t.status === 'completed' ? 'bg-green-200 text-green-700' :
              t.status === 'in_progress' ? 'bg-yellow-200 text-yellow-700' :
              'bg-gray-200 text-gray-500'
            }`}>
              {t.status === 'completed' ? '✓' : t.status === 'in_progress' ? '▶' : '○'}
            </span>
            <span className="text-indigo-800">{t.content}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function SearchToolView({ name, input }: { name: string; input: Record<string, unknown> }) {
  return (
    <div className="flex items-center gap-2 px-3 py-2 rounded border border-teal-200 bg-teal-50 text-sm">
      <span className="text-teal-600 text-xs font-bold font-mono">{name}</span>
      <code className="text-teal-800 text-xs">{input.pattern as string || input.query as string || JSON.stringify(input)}</code>
    </div>
  )
}

function GenericToolView({ name, description, input }: { name: string; description?: string; input: Record<string, unknown> }) {
  const [open, setOpen] = useState(false)
  const inputStr = JSON.stringify(input, null, 2)
  return (
    <div className="rounded border border-amber-200 bg-amber-50 text-sm">
      <div className="flex items-center gap-2 px-3 py-1.5">
        <span className="px-1.5 py-0.5 bg-amber-200 text-amber-800 rounded text-xs font-bold font-mono">{name}</span>
        {description && <span className="text-xs text-amber-700">{description}</span>}
        {inputStr.length > 100 && (
          <button onClick={() => setOpen(!open)} className="ml-auto text-xs text-amber-500 hover:text-amber-700">
            {open ? 'Hide' : 'Details'}
          </button>
        )}
      </div>
      {(open || inputStr.length <= 100) && (
        <pre className="px-3 py-1.5 text-xs text-amber-700 whitespace-pre-wrap overflow-x-auto border-t border-amber-200 max-h-40 overflow-y-auto">{inputStr}</pre>
      )}
    </div>
  )
}

export function ToolResultView({ result }: { result: unknown }) {
  const [open, setOpen] = useState(false)
  const resultStr = typeof result === 'string' ? result : JSON.stringify(result, null, 2)
  const isLong = resultStr.length > 300

  return (
    <div className="rounded border border-green-200 bg-green-50 text-sm">
      <div className="flex items-center gap-2 px-3 py-1.5">
        <span className="px-1.5 py-0.5 bg-green-200 text-green-800 rounded text-xs font-bold font-mono">OUTPUT</span>
        {isLong && (
          <button onClick={() => setOpen(!open)} className="ml-auto text-xs text-green-500 hover:text-green-700">
            {open ? 'Collapse' : `Expand (${resultStr.length} chars)`}
          </button>
        )}
      </div>
      {(!isLong || open) && (
        <pre className="px-3 py-1.5 text-xs text-green-800 whitespace-pre-wrap border-t border-green-200 max-h-60 overflow-y-auto">{resultStr}</pre>
      )}
    </div>
  )
}
