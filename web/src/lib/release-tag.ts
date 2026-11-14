/**
 * The release tag that matches a server version, for links and CI snippets
 * that pin the Clavis action or install script: "0.3.3" → "v0.3.3", and a
 * development build "0.3.2+dev.752e7e5" → "v0.3.2", the release it comes
 * after. Unknown versions use "main".
 */
export function releaseTag(version: string | null | undefined): string {
  const match = /^v?(\d+\.\d+\.\d+)/.exec(version ?? '')
  return match ? `v${match[1] ?? ''}` : 'main'
}
