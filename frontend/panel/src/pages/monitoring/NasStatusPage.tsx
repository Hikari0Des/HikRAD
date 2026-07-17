import { useParams } from 'react-router-dom'

import { getNasProbes } from '../../api/monitoring'
import { getNas } from '../../api/nas'
import { useAsync } from '../../hooks/useAsync'
import { StatusView } from './StatusView'

/** Per-NAS status page (FR-33): probe history for a registered NAS. */
export function NasStatusPage() {
  const { id = '' } = useParams()
  // The route only carries the uuid; resolve the NAS so the operator sees its
  // name, never the raw id (owner report 2026-07-17, known-issues).
  const nas = useAsync(() => getNas(id), [id])
  return (
    <StatusView
      title={nas.data ? nas.data.name : ''}
      subtitle={nas.data?.ip}
      queryKey={id}
      fetcher={() => getNasProbes(id)}
    />
  )
}
