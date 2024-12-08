import { Report } from '../types';
import ReportCard from './ReportCard';

interface ReportListProps {
  reports: Report[];
}

export default function ReportList({ reports }: ReportListProps) {
  if (reports.length === 0) {
    return (
      <div className="bg-blue-50 border border-blue-200 rounded-lg p-4">
        <p className="text-blue-700">No reports available for this channel.</p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {reports.map(report => (
        <ReportCard key={report.id} report={report} />
      ))}
    </div>
  );
} 