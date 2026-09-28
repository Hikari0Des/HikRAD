import { type TextareaHTMLAttributes } from 'react'

import { CONTROL } from './shared'

export function Textarea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea {...props} className={`${CONTROL} ${props.className ?? ''}`} />
}
