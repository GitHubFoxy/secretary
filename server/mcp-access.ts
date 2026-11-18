export function allowedMcpRequest(request: Request, port = 3001) {
  const host = request.headers.get('host')?.toLowerCase();
  const contentType = request.headers.get('content-type')?.split(';', 1)[0].trim().toLowerCase();
  return !request.headers.has('origin') &&
    (host === `127.0.0.1:${port}` || host === `localhost:${port}`) &&
    contentType === 'application/json';
}
