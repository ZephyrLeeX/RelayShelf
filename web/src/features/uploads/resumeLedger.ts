import type { ResumeEntry } from './types'

const PREFIX = 'relayshelf.upload-resume.v1:'

export class ResumeLedger {
  read(userId: string): ResumeEntry[] {
    try {
      const value = JSON.parse(localStorage.getItem(PREFIX + userId) ?? '[]') as unknown
      if (!Array.isArray(value)) return []
      return value.filter((entry): entry is ResumeEntry => Boolean(entry && typeof entry === 'object' && typeof (entry as ResumeEntry).uploadId === 'string' && typeof (entry as ResumeEntry).lastModified === 'number' && typeof (entry as ResumeEntry).createdAt === 'string'))
    } catch { return [] }
  }

  write(userId: string, entries: ResumeEntry[]) {
    localStorage.setItem(PREFIX + userId, JSON.stringify(entries.map(({ uploadId, lastModified, fingerprint, createdAt }) => ({ uploadId, lastModified, ...(fingerprint ? { fingerprint } : {}), createdAt }))))
  }

  upsert(userId: string, entry: ResumeEntry) {
    this.write(userId, [...this.read(userId).filter((item) => item.uploadId !== entry.uploadId), entry])
  }

  remove(userId: string, uploadId: string) { this.write(userId, this.read(userId).filter((item) => item.uploadId !== uploadId)) }
}
