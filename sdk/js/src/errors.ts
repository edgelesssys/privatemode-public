/** Error returned when a Privatemode API endpoint responds unsuccessfully. */
export class PrivatemodeAPIError extends Error {
  public readonly name = 'PrivatemodeAPIError' as const;

  public constructor(
    message: string,
    public readonly status: number,
    public readonly body: unknown,
    public readonly headers?: Headers,
    options?: ErrorOptions,
  ) {
    super(message, options);
  }
}
