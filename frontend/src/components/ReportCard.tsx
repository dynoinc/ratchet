import { Report } from '../types';

interface ReportCardProps {
  report: Report;
}

export default function ReportCard({ report }: ReportCardProps) {
  return (
    <div className="bg-white shadow rounded-lg p-6">
      <div className="space-y-2">
        <p className="font-medium">Report ID: {report.id}</p>
          
        <p className="text-gray-500">
          Generated: {new Date(report.createdAt).toLocaleString()}
        </p>
      </div>
    </div>
  );
} 