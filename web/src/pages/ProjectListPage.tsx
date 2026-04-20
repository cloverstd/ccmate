import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useState } from 'react'
import { projectsApi, githubApi, modelsApi, settingsApi, promptsApi, type Project, type GitHubRepo, type AgentProfile } from '../lib/api'
import { Card, CardHeader, CardContent, CardFooter, Label, Input, Select, Checkbox, Btn, Tag } from '../components/ui'

export default function ProjectListPage() {
  const queryClient = useQueryClient()
  const [showForm, setShowForm] = useState(false)
  const [selectedRepo, setSelectedRepo] = useState<GitHubRepo | null>(null)
  const [repoSearch, setRepoSearch] = useState('')
  const [repoDropdownOpen, setRepoDropdownOpen] = useState(false)
  const [autoMode, setAutoMode] = useState(false)
  const [defaultAgentProfileID, setDefaultAgentProfileID] = useState('')
  const [promptTemplateScope, setPromptTemplateScope] = useState<'global_only' | 'project_only' | 'merged'>('project_only')
  const [defaultPromptTemplateID, setDefaultPromptTemplateID] = useState('')

  const { data: projects, isLoading } = useQuery({ queryKey: ['projects'], queryFn: projectsApi.list })
  const { data: repos, isLoading: reposLoading } = useQuery({ queryKey: ['github-repos'], queryFn: githubApi.listRepos, enabled: showForm })
  const { data: models } = useQuery({ queryKey: ['models'], queryFn: modelsApi.list, enabled: showForm })
  const { data: settings } = useQuery({ queryKey: ['settings'], queryFn: settingsApi.get, enabled: showForm })
  const { data: globalTemplates } = useQuery({ queryKey: ['prompt-templates', 'global'], queryFn: () => promptsApi.list({ scope: 'global' }), enabled: showForm })

  const createMutation = useMutation({
    mutationFn: (data: Partial<Project>) => projectsApi.create(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      setShowForm(false); setSelectedRepo(null); setDefaultAgentProfileID('')
      setPromptTemplateScope('project_only'); setDefaultPromptTemplateID('')
    },
  })

  const handleCreate = () => {
    if (!selectedRepo) return
    createMutation.mutate({
      name: selectedRepo.full_name, repo_url: selectedRepo.html_url, git_provider: 'github',
      default_branch: selectedRepo.default_branch, auto_mode: autoMode,
      default_agent_profile_id: defaultAgentProfileID ? parseInt(defaultAgentProfileID, 10) : undefined,
      prompt_template_scope: promptTemplateScope,
      default_prompt_template_id: defaultPromptTemplateID ? parseInt(defaultPromptTemplateID, 10) : null,
    })
  }

  return (
    <div className="max-w-4xl mx-auto">
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">Projects</h1>
        <Btn onClick={() => setShowForm(!showForm)}>{showForm ? 'Cancel' : 'New Project'}</Btn>
      </div>

      {showForm && (
        <Card>
          <CardHeader title="Add Project" description="Select a GitHub repository to manage" />
          <CardContent>
            <div className="relative">
              <Label>Repository</Label>
              {reposLoading ? (
                <p className="text-sm text-gray-400 py-2">Loading repositories...</p>
              ) : (
                <div>
                  <Input
                    value={selectedRepo ? selectedRepo.full_name : repoSearch}
                    onChange={(e) => { setRepoSearch(e.target.value); setSelectedRepo(null); setRepoDropdownOpen(true) }}
                    onFocus={() => setRepoDropdownOpen(true)}
                    onBlur={() => setTimeout(() => setRepoDropdownOpen(false), 200)}
                    placeholder="Search repositories..." autoComplete="off"
                  />
                  {repoDropdownOpen && !selectedRepo && (
                    <div className="absolute z-10 w-full mt-1 bg-white border border-gray-200 rounded-lg shadow-lg max-h-60 overflow-y-auto">
                      {(repos || [])
                        .filter((repo) => !repoSearch || repo.full_name.toLowerCase().includes(repoSearch.toLowerCase()) || (repo.description || '').toLowerCase().includes(repoSearch.toLowerCase()))
                        .map((repo) => (
                          <button key={repo.full_name} type="button"
                            onClick={() => { setSelectedRepo(repo); setRepoSearch(''); setRepoDropdownOpen(false) }}
                            className="w-full text-left px-3 py-2.5 hover:bg-blue-50 text-sm border-b border-gray-50 last:border-0 transition-colors">
                            <div className="font-medium text-gray-900">{repo.full_name} {repo.private && <Tag color="gray">private</Tag>}</div>
                            {repo.description && <div className="text-xs text-gray-500 truncate mt-0.5">{repo.description}</div>}
                          </button>
                        ))}
                      {(repos || []).filter((r) => !repoSearch || r.full_name.toLowerCase().includes(repoSearch.toLowerCase())).length === 0 && (
                        <div className="px-3 py-3 text-sm text-gray-400 text-center">No matching repositories</div>
                      )}
                    </div>
                  )}
                </div>
              )}
            </div>

            {selectedRepo && (
              <div className="space-y-4">
                <div className="flex flex-wrap gap-2">
                  <Tag>{selectedRepo.full_name}</Tag>
                  <Tag color="gray">{selectedRepo.default_branch}</Tag>
                </div>
                <Checkbox label="Auto Mode" checked={autoMode} onChange={setAutoMode}
                  description="Automatically create tasks when issue labels match" />
                <div>
                  <Label>Default Agent</Label>
                  <Select value={defaultAgentProfileID} onChange={(e) => setDefaultAgentProfileID(e.target.value)} className="w-full max-w-md">
                    <option value="">Use global default</option>
                    {(models || []).map((m: AgentProfile) => <option key={m.id} value={String(m.id)}>{m.provider} / {m.model || 'default'}</option>)}
                  </Select>
                  <p className="text-xs text-gray-400 mt-1">
                    {defaultAgentProfileID ? 'Tasks will use this agent.' : `Tasks will use the global default${settings?.default_agent_profile_id ? '' : ' if configured'}.`}
                  </p>
                </div>
                <div>
                  <Label>Template Scope</Label>
                  <Select value={promptTemplateScope} onChange={(e) => setPromptTemplateScope(e.target.value as 'global_only' | 'project_only' | 'merged')} className="w-full max-w-md">
                    <option value="project_only">Project Only</option>
                    <option value="global_only">Global Only</option>
                    <option value="merged">Merged</option>
                  </Select>
                  <p className="text-xs text-gray-400 mt-1">Controls which prompt templates apply to tasks in this project.</p>
                </div>
                {promptTemplateScope !== 'global_only' && (
                  <div>
                    <Label>Project Template</Label>
                    <Select value={defaultPromptTemplateID} onChange={(e) => setDefaultPromptTemplateID(e.target.value)} className="w-full max-w-md">
                      <option value="">Platform default</option>
                      {(globalTemplates || []).map((t) => (
                        <option key={t.id} value={String(t.id)}>{t.name} (global)</option>
                      ))}
                    </Select>
                    <p className="text-xs text-gray-400 mt-1">Pick a global template now, or add project-specific templates after creation.</p>
                  </div>
                )}
              </div>
            )}
          </CardContent>
          {selectedRepo && (
            <CardFooter>
              <Btn onClick={handleCreate} disabled={createMutation.isPending}>
                {createMutation.isPending ? 'Creating...' : 'Create Project'}
              </Btn>
            </CardFooter>
          )}
        </Card>
      )}

      {isLoading ? (
        <p className="text-gray-400 text-sm text-center py-8">Loading...</p>
      ) : !projects || projects.length === 0 ? (
        <Card>
          <CardContent className="!py-12">
            <p className="text-gray-400 text-center text-sm">No projects yet. Add one to get started.</p>
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-3">
          {projects.map((project) => (
            <Link key={project.id} to={`/projects/${project.id}`}
              className="block rounded-xl border border-gray-200 bg-white shadow-sm hover:border-gray-300 hover:shadow transition-all p-4">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3 min-w-0">
                  <span className="text-sm font-semibold text-gray-900 truncate">{project.name}</span>
                  <Tag color={project.auto_mode ? 'green' : 'gray'}>{project.auto_mode ? 'Auto' : 'Manual'}</Tag>
                </div>
                <span className="text-xs text-gray-400 shrink-0 ml-4 font-mono">{project.default_branch}</span>
              </div>
              <p className="text-xs text-gray-500 mt-1 truncate">{project.repo_url}</p>
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}
