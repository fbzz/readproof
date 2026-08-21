export class ReadproofError extends Error {
  readonly status?: number;

  constructor(message: string, status?: number) {
    super(message);
    this.name = "ReadproofError";
    this.status = status;
  }
}
