import type { CSSProperties } from 'react'

type IconName =
  | 'terminal' | 'folder' | 'list' | 'settings' | 'logout' | 'chevron' | 'chevdn'
  | 'close' | 'plus' | 'search' | 'play' | 'pause' | 'refresh' | 'check' | 'warn'
  | 'sparkles' | 'github' | 'git' | 'branch' | 'commit' | 'issue' | 'pr' | 'send'
  | 'paperclip' | 'image' | 'sun' | 'moon' | 'sliders' | 'stop' | 'retry' | 'user'
  | 'bot' | 'tool' | 'copy' | 'ext' | 'more' | 'menu' | 'info' | 'back' | 'key' | 'layers'

const P = {
  fill: 'none' as const,
  stroke: 'currentColor',
  strokeWidth: 1.6,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
}

const PATHS: Record<IconName, React.ReactNode> = {
  terminal: <><polyline {...P} points="4 7 8 11 4 15" /><line {...P} x1="11" y1="16" x2="17" y2="16" /></>,
  folder:   <path {...P} d="M3 6.5A1.5 1.5 0 0 1 4.5 5h3l2 2h6A1.5 1.5 0 0 1 17 8.5v7A1.5 1.5 0 0 1 15.5 17h-11A1.5 1.5 0 0 1 3 15.5Z" />,
  list:     <><line {...P} x1="6" y1="6" x2="17" y2="6"/><line {...P} x1="6" y1="10" x2="17" y2="10"/><line {...P} x1="6" y1="14" x2="17" y2="14"/><circle cx="3.5" cy="6" r="0.8" fill="currentColor"/><circle cx="3.5" cy="10" r="0.8" fill="currentColor"/><circle cx="3.5" cy="14" r="0.8" fill="currentColor"/></>,
  settings: <><circle {...P} cx="10" cy="10" r="2.2"/><path {...P} d="M10 3v1.8M10 15.2V17M3 10h1.8M15.2 10H17M5.05 5.05l1.27 1.27M13.68 13.68l1.27 1.27M5.05 14.95l1.27-1.27M13.68 6.32l1.27-1.27"/></>,
  logout:   <><path {...P} d="M8 4H5a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h3"/><path {...P} d="M12 7l3 3-3 3"/><line {...P} x1="9" y1="10" x2="15" y2="10"/></>,
  chevron:  <polyline {...P} points="7 4 13 10 7 16"/>,
  chevdn:   <polyline {...P} points="4 7 10 13 16 7"/>,
  close:    <><line {...P} x1="5" y1="5" x2="15" y2="15"/><line {...P} x1="15" y1="5" x2="5" y2="15"/></>,
  plus:     <><line {...P} x1="10" y1="4" x2="10" y2="16"/><line {...P} x1="4" y1="10" x2="16" y2="10"/></>,
  search:   <><circle {...P} cx="9" cy="9" r="5"/><line {...P} x1="13" y1="13" x2="16" y2="16"/></>,
  play:     <path {...P} d="M6 4.5v11l9-5.5z"/>,
  pause:    <><rect {...P} x="5" y="4" width="3" height="12" rx="0.5"/><rect {...P} x="12" y="4" width="3" height="12" rx="0.5"/></>,
  refresh:  <><path {...P} d="M4 10a6 6 0 0 1 10.5-4"/><polyline {...P} points="15 3 15 6.5 11.5 6.5"/><path {...P} d="M16 10a6 6 0 0 1-10.5 4"/><polyline {...P} points="5 17 5 13.5 8.5 13.5"/></>,
  check:    <polyline {...P} points="4 10 8 14 16 6"/>,
  warn:     <><path {...P} d="M10 3l7.5 13h-15z"/><line {...P} x1="10" y1="8" x2="10" y2="12"/><circle cx="10" cy="14.5" r="0.9" fill="currentColor"/></>,
  sparkles: <path {...P} d="M10 3v4M10 13v4M3 10h4M13 10h4M5.5 5.5l2 2M12.5 12.5l2 2M5.5 14.5l2-2M12.5 7.5l2-2"/>,
  github:   <path fill="currentColor" d="M10 0a10 10 0 0 0-3.2 19.5c.5.1.7-.2.7-.5v-1.7c-2.8.6-3.4-1.3-3.4-1.3-.4-1.2-1.1-1.5-1.1-1.5-.9-.6.1-.6.1-.6 1 .1 1.5 1 1.5 1 .9 1.5 2.4 1.1 3 .8.1-.7.4-1.1.6-1.4-2.2-.3-4.5-1.1-4.5-4.9 0-1.1.4-2 1-2.7-.1-.3-.4-1.3.1-2.7 0 0 .8-.3 2.7 1a9.4 9.4 0 0 1 5 0c1.9-1.3 2.7-1 2.7-1 .5 1.4.2 2.4.1 2.7a4 4 0 0 1 1 2.7c0 3.9-2.3 4.7-4.5 5 .4.3.7 1 .7 1.9v2.9c0 .3.2.6.7.5A10 10 0 0 0 10 0"/>,
  git:      <><circle {...P} cx="5" cy="10" r="1.8"/><circle {...P} cx="15" cy="5" r="1.8"/><circle {...P} cx="15" cy="15" r="1.8"/><path {...P} d="M5 8V6a2 2 0 0 1 2-2h6"/><path {...P} d="M13 16H7a2 2 0 0 1-2-2v-2"/></>,
  branch:   <><circle {...P} cx="6" cy="5" r="1.5"/><circle {...P} cx="6" cy="15" r="1.5"/><circle {...P} cx="14" cy="7" r="1.5"/><line {...P} x1="6" y1="6.5" x2="6" y2="13.5"/><path {...P} d="M6 11a8 8 0 0 0 8-2.5"/></>,
  commit:   <><circle {...P} cx="10" cy="10" r="2.5"/><line {...P} x1="3" y1="10" x2="7" y2="10"/><line {...P} x1="13" y1="10" x2="17" y2="10"/></>,
  issue:    <><circle {...P} cx="10" cy="10" r="6"/><circle cx="10" cy="10" r="1.2" fill="currentColor"/></>,
  pr:       <><circle {...P} cx="6" cy="5" r="1.5"/><circle {...P} cx="6" cy="15" r="1.5"/><circle {...P} cx="14" cy="15" r="1.5"/><line {...P} x1="6" y1="6.5" x2="6" y2="13.5"/><path {...P} d="M14 13V9a4 4 0 0 0-4-4H9"/><polyline {...P} points="11 3 9 5 11 7"/></>,
  send:     <><path {...P} d="M3 10l14-6-5 14-3-6z"/><line {...P} x1="9" y1="11" x2="17" y2="4"/></>,
  paperclip:<path {...P} d="M14 8l-5 5a2 2 0 0 1-3-3l6-6a3 3 0 0 1 4 4l-7 7a4 4 0 0 1-6-6l5-5"/>,
  image:    <><rect {...P} x="3" y="4" width="14" height="12" rx="1.5"/><circle {...P} cx="7" cy="8.5" r="1.2"/><polyline {...P} points="3 14 7.5 10 11 13 14 11 17 14"/></>,
  sun:      <><circle {...P} cx="10" cy="10" r="3.2"/><line {...P} x1="10" y1="3" x2="10" y2="5"/><line {...P} x1="10" y1="15" x2="10" y2="17"/><line {...P} x1="3" y1="10" x2="5" y2="10"/><line {...P} x1="15" y1="10" x2="17" y2="10"/><line {...P} x1="5" y1="5" x2="6.5" y2="6.5"/><line {...P} x1="13.5" y1="13.5" x2="15" y2="15"/><line {...P} x1="5" y1="15" x2="6.5" y2="13.5"/><line {...P} x1="13.5" y1="6.5" x2="15" y2="5"/></>,
  moon:     <path {...P} d="M15 11.5A6 6 0 0 1 8.5 5a5 5 0 1 0 6.5 6.5z"/>,
  sliders:  <><line {...P} x1="4" y1="6" x2="16" y2="6"/><line {...P} x1="4" y1="10" x2="16" y2="10"/><line {...P} x1="4" y1="14" x2="16" y2="14"/><circle cx="8" cy="6" r="1.8" fill="var(--bg)" stroke="currentColor" strokeWidth="1.5"/><circle cx="13" cy="10" r="1.8" fill="var(--bg)" stroke="currentColor" strokeWidth="1.5"/><circle cx="6" cy="14" r="1.8" fill="var(--bg)" stroke="currentColor" strokeWidth="1.5"/></>,
  stop:     <rect {...P} x="5" y="5" width="10" height="10" rx="1"/>,
  retry:    <><polyline {...P} points="15 3 15 7 11 7"/><path {...P} d="M15 7a6 6 0 1 0 1 7"/></>,
  user:     <><circle {...P} cx="10" cy="7" r="3"/><path {...P} d="M4 17a6 6 0 0 1 12 0"/></>,
  bot:      <><rect {...P} x="4" y="7" width="12" height="9" rx="2"/><line {...P} x1="10" y1="4" x2="10" y2="7"/><circle cx="7.5" cy="11.5" r="0.9" fill="currentColor"/><circle cx="12.5" cy="11.5" r="0.9" fill="currentColor"/></>,
  tool:     <><path {...P} d="M13 3l4 4-3 3-4-4zm-3 4L3 14v3h3l7-7"/></>,
  copy:     <><rect {...P} x="6" y="6" width="10" height="10" rx="1"/><path {...P} d="M4 12V5a1 1 0 0 1 1-1h7"/></>,
  ext:      <><path {...P} d="M12 4h4v4"/><line {...P} x1="16" y1="4" x2="10" y2="10"/><path {...P} d="M14 12v3a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h3"/></>,
  more:     <><circle cx="5" cy="10" r="1.3" fill="currentColor"/><circle cx="10" cy="10" r="1.3" fill="currentColor"/><circle cx="15" cy="10" r="1.3" fill="currentColor"/></>,
  menu:     <><line {...P} x1="3" y1="6" x2="17" y2="6"/><line {...P} x1="3" y1="10" x2="17" y2="10"/><line {...P} x1="3" y1="14" x2="17" y2="14"/></>,
  info:     <><circle {...P} cx="10" cy="10" r="6.5"/><line {...P} x1="10" y1="9" x2="10" y2="13"/><circle cx="10" cy="7" r="0.9" fill="currentColor"/></>,
  back:     <polyline {...P} points="12 4 6 10 12 16"/>,
  key:      <><circle {...P} cx="6.5" cy="10" r="2.5"/><line {...P} x1="9" y1="10" x2="16" y2="10"/><line {...P} x1="14" y1="10" x2="14" y2="13"/><line {...P} x1="16" y1="10" x2="16" y2="12"/></>,
  layers:   <><path {...P} d="M10 3l7 3.5-7 3.5-7-3.5z"/><path {...P} d="M3 10l7 3.5 7-3.5"/><path {...P} d="M3 13.5l7 3.5 7-3.5"/></>,
}

export default function Icon({ name, size = 14, style, className }: {
  name: IconName; size?: number; style?: CSSProperties; className?: string
}) {
  return (
    <svg viewBox="0 0 20 20" width={size} height={size} className={className} style={style} aria-hidden="true">
      {PATHS[name]}
    </svg>
  )
}

export function Logo({ size = 28, style }: { size?: number; style?: CSSProperties }) {
  return (
    <svg viewBox="0 0 32 32" width={size} height={size} style={style} aria-label="ccmate">
      <rect x="2" y="4" width="28" height="24" rx="4" fill="var(--fg)"/>
      <rect x="2" y="4" width="28" height="5" rx="4" fill="var(--accent)"/>
      <circle cx="5.5" cy="6.5" r="0.9" fill="var(--bg)" opacity="0.6"/>
      <circle cx="8.5" cy="6.5" r="0.9" fill="var(--bg)" opacity="0.6"/>
      <circle cx="11.5" cy="6.5" r="0.9" fill="var(--bg)" opacity="0.6"/>
      <path d="M7 15l3 3-3 3" fill="none" stroke="var(--accent)" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"/>
      <rect x="13" y="21" width="10" height="1.6" rx="0.3" fill="var(--bg)" opacity="0.85"/>
    </svg>
  )
}
