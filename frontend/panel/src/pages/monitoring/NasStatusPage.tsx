import { useParams } from 'react-router-dom'

import { getNasProbes } from '../../api/monitoring'
import { StatusView } from './StatusView'

/** Per-NAS status page (FR-33): probe history for a registered NAS. */
export function NasStatusPage() {
  const { id = '' } = useParams()
  return <StatusView title={id} fetcher={() => getNasProbes(id)} />
}
