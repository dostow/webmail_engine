import { useRouteError, isRouteErrorResponse, useNavigate } from 'react-router-dom';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/Alert';

export function MessagesErrorBoundary() {
  const error = useRouteError();
  const navigate = useNavigate();

  // Handle route error responses (like redirects)
  if (isRouteErrorResponse(error)) {
    return (
      <Card>
        <div className="p-12 text-center">
          <div className="text-5xl mb-4">⚠️</div>
          <h2 className="text-xl font-semibold mb-2">
            {error.status === 401 ? 'Authentication Required' : 'Error Loading Messages'}
          </h2>
          <p className="text-muted-foreground mb-4">
            {error.status === 401
              ? 'This account requires authentication. Redirecting...'
              : error.statusText || 'An unexpected error occurred'}
          </p>
          <Button variant="outline" onClick={() => navigate('/accounts')}>
            Back to Accounts
          </Button>
        </div>
      </Card>
    );
  }

  // Handle authentication errors
  const isAuthError =
    error instanceof Error &&
    (error as any).code === 'AUTH_ERROR';

  if (isAuthError) {
    return (
      <div className="p-6">
        <Alert variant="warning">
          <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
          </svg>
          <div>
            <AlertTitle>Account Authentication Required</AlertTitle>
            <AlertDescription>
              Please update your credentials to access messages. Redirecting to account settings...
            </AlertDescription>
          </div>
        </Alert>
      </div>
    );
  }

  // Generic error fallback
  return (
    <Card>
      <div className="p-12 text-center">
        <div className="text-5xl mb-4">❌</div>
        <h2 className="text-xl font-semibold mb-2">Failed to Load Messages</h2>
        <p className="text-muted-foreground mb-4">
          {error instanceof Error ? error.message : 'An unexpected error occurred'}
        </p>
        <Button variant="outline" onClick={() => navigate('/accounts')}>
          Back to Accounts
        </Button>
      </div>
    </Card>
  );
}
