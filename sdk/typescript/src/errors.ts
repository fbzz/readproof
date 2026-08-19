export class CtxError extends Error {
  readonly status?: number;

  constructor(message: string, status?: number) {
    super(message);
    this.name = "CtxError";
    this.status = status;
  }
}
