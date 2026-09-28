import { type InputHTMLAttributes } from 'react'

import { CONTROL } from './shared'

/** Plain text/number/date/etc input — native element (not CI-gated, see the
 * phase brief's C4), restyled with a visible focus ring (C8). */
export function TextInput(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={`${CONTROL} ${props.className ?? ''}`} />
}
