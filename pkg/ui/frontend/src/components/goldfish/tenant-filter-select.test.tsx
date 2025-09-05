import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { TenantFilterSelect } from './tenant-filter-select';
import '@testing-library/jest-dom';

describe('TenantFilterSelect', () => {
  const mockOnChange = jest.fn();
  const mockTenants = ['tenant-a', 'tenant-b', 'tenant-c', 'tenant-123'];

  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('rendering', () => {
    it('renders with no selection (All Tenants)', () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      expect(select).toBeInTheDocument();
      expect(screen.getByText('All Tenants')).toBeInTheDocument();
    });

    it('renders with a selected tenant', () => {
      render(
        <TenantFilterSelect 
          value="tenant-b" 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      expect(screen.getByText('tenant-b')).toBeInTheDocument();
    });

    it('renders all tenant options when opened', () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      fireEvent.click(select);
      
      // Should show All Tenants option plus all provided tenants
      expect(screen.getByText('All Tenants')).toBeInTheDocument();
      mockTenants.forEach(tenant => {
        expect(screen.getAllByText(tenant).length).toBeGreaterThan(0);
      });
    });

    it('renders with empty tenant list', () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={[]} 
        />
      );
      
      expect(screen.getByRole('combobox')).toBeInTheDocument();
      expect(screen.getByText('All Tenants')).toBeInTheDocument();
    });
  });

  describe('selection', () => {
    it('selects a tenant when clicked', async () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      fireEvent.click(select);
      
      const tenantOption = screen.getAllByText('tenant-b')[0];
      fireEvent.click(tenantOption);
      
      expect(mockOnChange).toHaveBeenCalledWith('tenant-b');
    });

    it('selects All Tenants (undefined) when clicked', async () => {
      render(
        <TenantFilterSelect 
          value="tenant-a" 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      fireEvent.click(select);
      
      const allOption = screen.getByText('All Tenants');
      fireEvent.click(allOption);
      
      expect(mockOnChange).toHaveBeenCalledWith(undefined);
    });

    it('maintains selection after closing and reopening', async () => {
      render(
        <TenantFilterSelect 
          value="tenant-c" 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      expect(screen.getByText('tenant-c')).toBeInTheDocument();
      
      // Open and close dropdown
      const select = screen.getByRole('combobox');
      fireEvent.click(select);
      fireEvent.click(document.body); // Click outside to close
      
      // Should still show selected value
      expect(screen.getByText('tenant-c')).toBeInTheDocument();
    });
  });

  describe('clear functionality', () => {
    it('shows clear button when a tenant is selected', () => {
      render(
        <TenantFilterSelect 
          value="tenant-a" 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const clearButton = screen.getByRole('button', { name: /clear/i });
      expect(clearButton).toBeInTheDocument();
    });

    it('does not show clear button when no tenant is selected', () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      expect(screen.queryByRole('button', { name: /clear/i })).not.toBeInTheDocument();
    });

    it('clears selection when clear button is clicked', () => {
      render(
        <TenantFilterSelect 
          value="tenant-b" 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const clearButton = screen.getByRole('button', { name: /clear/i });
      fireEvent.click(clearButton);
      
      expect(mockOnChange).toHaveBeenCalledWith(undefined);
    });

    it('prevents dropdown from opening when clicking clear button', () => {
      render(
        <TenantFilterSelect 
          value="tenant-a" 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const clearButton = screen.getByRole('button', { name: /clear/i });
      fireEvent.click(clearButton);
      
      // Dropdown should not be open
      expect(screen.getAllByText('tenant-b')).toHaveLength(0);
    });
  });

  describe('sorting', () => {
    it('sorts tenant options alphabetically', () => {
      const unsortedTenants = ['zebra', 'alpha', 'charlie', 'bravo'];
      
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={unsortedTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      fireEvent.click(select);
      
      const options = screen.getAllByRole('option');
      // First option should be "All Tenants", then sorted tenants
      expect(options[0]).toHaveTextContent('All Tenants');
      expect(options[1]).toHaveTextContent('alpha');
      expect(options[2]).toHaveTextContent('bravo');
      expect(options[3]).toHaveTextContent('charlie');
      expect(options[4]).toHaveTextContent('zebra');
    });
  });

  describe('search/filter', () => {
    it('filters tenants when typing', async () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      fireEvent.click(select);
      
      // Type to filter
      const searchInput = screen.getByRole('textbox');
      fireEvent.change(searchInput, { target: { value: 'tenant-1' } });
      
      await waitFor(() => {
        // Should only show matching tenant
        expect(screen.getByText('tenant-123')).toBeInTheDocument();
        expect(screen.queryByText('tenant-a')).not.toBeInTheDocument();
        expect(screen.queryByText('tenant-b')).not.toBeInTheDocument();
      });
    });

    it('shows no results message when filter matches nothing', async () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      fireEvent.click(select);
      
      const searchInput = screen.getByRole('textbox');
      fireEvent.change(searchInput, { target: { value: 'xyz-no-match' } });
      
      await waitFor(() => {
        expect(screen.getByText(/No tenants found/i)).toBeInTheDocument();
      });
    });

    it('resets filter when dropdown is closed', async () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      fireEvent.click(select);
      
      // Filter tenants
      const searchInput = screen.getByRole('textbox');
      fireEvent.change(searchInput, { target: { value: 'tenant-a' } });
      
      // Close dropdown
      fireEvent.click(document.body);
      
      // Reopen dropdown
      fireEvent.click(select);
      
      // All tenants should be visible again
      mockTenants.forEach(tenant => {
        expect(screen.getAllByText(tenant).length).toBeGreaterThan(0);
      });
    });
  });

  describe('keyboard navigation', () => {
    it('navigates options with arrow keys', async () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      fireEvent.click(select);
      
      // Arrow down should highlight first tenant option
      fireEvent.keyDown(select, { key: 'ArrowDown' });
      
      // Enter should select the highlighted option
      fireEvent.keyDown(select, { key: 'Enter' });
      
      expect(mockOnChange).toHaveBeenCalled();
    });

    it('closes dropdown with Escape key', async () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      fireEvent.click(select);
      
      // Dropdown should be open
      expect(screen.getAllByText('tenant-a').length).toBeGreaterThan(1);
      
      fireEvent.keyDown(select, { key: 'Escape' });
      
      // Dropdown should be closed
      await waitFor(() => {
        expect(screen.queryByRole('option')).not.toBeInTheDocument();
      });
    });
  });

  describe('edge cases', () => {
    it('handles null onChange gracefully', () => {
      expect(() => {
        render(
          <TenantFilterSelect 
            value={undefined} 
            onChange={null as unknown as () => void} 
            tenants={mockTenants} 
          />
        );
      }).not.toThrow();
    });

    it('handles duplicate tenants', () => {
      const duplicateTenants = ['tenant-a', 'tenant-a', 'tenant-b', 'tenant-b'];
      
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={duplicateTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      fireEvent.click(select);
      
      // Should deduplicate tenants
      const options = screen.getAllByRole('option');
      // All Tenants + 2 unique tenants
      expect(options).toHaveLength(3);
    });

    it('handles special characters in tenant names', () => {
      const specialTenants = ['tenant-@#$', 'tenant_123', 'tenant.com', 'tenant/slash'];
      
      render(
        <TenantFilterSelect 
          value="tenant-@#$" 
          onChange={mockOnChange} 
          tenants={specialTenants} 
        />
      );
      
      expect(screen.getByText('tenant-@#$')).toBeInTheDocument();
      
      const select = screen.getByRole('combobox');
      fireEvent.click(select);
      
      specialTenants.forEach(tenant => {
        expect(screen.getAllByText(tenant).length).toBeGreaterThan(0);
      });
    });

    it('handles very long tenant names', () => {
      const longTenant = 'a'.repeat(100);
      
      render(
        <TenantFilterSelect 
          value={longTenant} 
          onChange={mockOnChange} 
          tenants={[longTenant]} 
        />
      );
      
      // Should display without crashing
      expect(screen.getByText(longTenant)).toBeInTheDocument();
    });

    it('handles empty string tenant name', () => {
      render(
        <TenantFilterSelect 
          value="" 
          onChange={mockOnChange} 
          tenants={['', 'tenant-a']} 
        />
      );
      
      // Should handle empty string as a valid tenant
      const select = screen.getByRole('combobox');
      fireEvent.click(select);
      
      const options = screen.getAllByRole('option');
      expect(options.length).toBeGreaterThanOrEqual(2);
    });
  });

  describe('accessibility', () => {
    it('has proper ARIA labels', () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      expect(select).toHaveAttribute('aria-expanded', 'false');
      
      fireEvent.click(select);
      expect(select).toHaveAttribute('aria-expanded', 'true');
    });

    it('supports screen reader announcements', () => {
      render(
        <TenantFilterSelect 
          value="tenant-a" 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      // Selected value should be announced
      expect(screen.getByText('tenant-a')).toBeInTheDocument();
    });
  });
});
