export class RenderInputError extends Error {
  readonly code = 'invalid_render_input';
}

export class RenderAssetError extends Error {
  readonly code = 'missing_render_asset';
}
