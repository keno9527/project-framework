import { useHealthStatus } from './hooks/use-health-status'
import './index.css'

export function HomePage() {
  const { connection, status } = useHealthStatus()

  return (
    <main className="app-shell">
      <div className="ambient ambient-one" aria-hidden="true" />
      <div className="ambient ambient-two" aria-hidden="true" />

      <section className="hero" aria-labelledby="page-title">
        <p className="eyebrow">Frontend foundation</p>
        <h1 id="page-title">Project Framework</h1>
        <p className="intro">
          React, TypeScript, and Vite are ready. Start building product features from a tested,
          typed baseline.
        </p>

        <div className={`status-card status-${connection.phase}`} aria-live="polite">
          <span className="status-indicator" aria-hidden="true" />
          <div>
            <p className="status-label">API status</p>
            <p className="status-value">{status.title}</p>
            <p className="status-detail">{status.detail}</p>
          </div>
        </div>
      </section>
    </main>
  )
}
