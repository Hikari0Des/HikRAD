/**
 * Client-side mirror of backend/internal/radius/ros_matrix.go's
 * rosMatrixValidated — used only to show/hide the disabled-apply explanation
 * before a round trip; the server re-checks this authoritatively on apply.
 */
export function rosMatrixValidated(rosVersion: string | null | undefined): boolean {
  const v = (rosVersion ?? '').trim()
  if (v === '') return false
  return v.startsWith('6') || v.startsWith('7')
}
