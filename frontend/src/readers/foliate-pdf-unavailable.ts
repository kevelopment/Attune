/** PDF is handled by Attune's separate PDF.js adapter, never by Foliate.js. */
export async function makePDF(): Promise<never> {
  throw new Error('PDF files must be opened with Attune’s fixed-layout reader.')
}
