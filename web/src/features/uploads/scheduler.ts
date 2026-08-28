interface ScheduledTask { fileId: string; run: () => Promise<void> }

export class UploadScheduler {
  private pending: ScheduledTask[] = []
  private activeGlobal = 0
  private activeByFile = new Map<string, number>()

  constructor(readonly globalLimit = 4, readonly perFileLimit = 2) {}

  enqueue(fileId: string, run: () => Promise<void>) {
    this.pending.push({ fileId, run })
    this.drain()
  }

  remove(fileId: string) { this.pending = this.pending.filter((task) => task.fileId !== fileId) }
  get active() { return this.activeGlobal }
  activeFor(fileId: string) { return this.activeByFile.get(fileId) ?? 0 }

  private drain() {
    while (this.activeGlobal < this.globalLimit) {
      const index = this.pending.findIndex((task) => this.activeFor(task.fileId) < this.perFileLimit)
      if (index < 0) return
      const [task] = this.pending.splice(index, 1)
      this.activeGlobal++
      this.activeByFile.set(task.fileId, this.activeFor(task.fileId) + 1)
      // Bookkeeping must always execute, and an unexpected task rejection must
      // never leave an unhandled rejected promise: both settlement paths run
      // the same cleanup and the chained promise resolves either way.
      void task.run().then(() => this.finish(task.fileId), () => this.finish(task.fileId))
    }
  }

  private finish(fileId: string) {
    this.activeGlobal--
    const remaining = this.activeFor(fileId) - 1
    if (remaining > 0) this.activeByFile.set(fileId, remaining); else this.activeByFile.delete(fileId)
    this.drain()
  }
}
