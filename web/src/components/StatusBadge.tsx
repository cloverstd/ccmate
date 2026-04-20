import type { TaskStatus } from '../lib/api'

export default function StatusBadge({ status }: { status: TaskStatus }) {
  return (
    <span className={`badge ${status}`}>
      <span className="d" />
      {status.replace('_', ' ')}
    </span>
  )
}
