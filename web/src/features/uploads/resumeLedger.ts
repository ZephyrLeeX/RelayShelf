import type { ResumeEntry } from './types'

const PREFIX = 'relayshelf.upload-resume.v1:'

// The ledger is convenience metadata only; server completedParts stay the
// upload authority. Storage failures must never alter an upload outcome, so
// every mutation is best-effort and reports availability instead of throwing.
export class ResumeLedger {
  private storageFailed = false

  get available() { return !this.storageFailed }

  read(userId: string): ResumeEntry[] {
    try {
      const value = JSON.parse(localStorage.getItem(PREFIX + userId) ?? '[]') as unknown
      if (!Array.isArray(value)) return []
      return value.filter((entry): entry is ResumeEntry => Boolean(entry && typeof entry === 'object' && typeof (entry as ResumeEntry).uploadId === 'string' && typeof (entry as ResumeEntry).lastModified === 'number' && typeof (entry as ResumeEntry).createdAt === 'string'))
    } catch {
      this.storageFailed = true
      return []
    }
  }

  write(userId: string, entries: ResumeEntry[]) {
    try {
      localStorage.setItem(PREFIX + userId, JSON.stringify(entries.map(({ uploadId, lastModified, fingerprint, createdAt }) => ({ uploadId, lastModified, ...(fingerprint ? { fingerprint } : {}), createdAt }))))
    } catch {
      this.storageFailed = true
    }
  }

  upsert(userId: string, entry: ResumeEntry) {
    this.write(userId, [...this.read(userId).filter((item) => item.uploadId !== entry.uploadId), entry])
  }

  remove(userId: string, uploadId: string) { this.write(userId, this.read(userId).filter((item) => item.uploadId !== uploadId)) }
}
